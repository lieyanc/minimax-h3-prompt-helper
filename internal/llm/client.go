// Package llm is a minimal client for OpenAI-compatible /chat/completions
// endpoints, covering both text and image (vision) messages.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to one OpenAI-compatible endpoint.
type Client struct {
	BaseURL     string
	APIKey      string
	Model       string
	MaxTokens   int
	Temperature float64
	HTTP        *http.Client
}

// New builds a client with a long timeout suited to streaming generations.
func New(baseURL, apiKey, model string, maxTokens int, temperature float64) *Client {
	return &Client{
		BaseURL:     strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		APIKey:      strings.TrimSpace(apiKey),
		Model:       strings.TrimSpace(model),
		MaxTokens:   maxTokens,
		Temperature: temperature,
		HTTP:        &http.Client{Timeout: 15 * time.Minute},
	}
}

// Part is one piece of a multimodal message.
type Part struct {
	Text     string
	ImageURL string // data: URL or remote URL
}

// Message is a chat message with one or more content parts.
type Message struct {
	Role  string
	Parts []Part
}

// Text builds a plain text message.
func Text(role, text string) Message {
	return Message{Role: role, Parts: []Part{{Text: text}}}
}

// MarshalJSON emits a bare string content for single text parts, which the
// widest range of compatible servers accept, and the array form otherwise.
func (m Message) MarshalJSON() ([]byte, error) {
	type outPart struct {
		Type     string            `json:"type"`
		Text     string            `json:"text,omitempty"`
		ImageURL map[string]string `json:"image_url,omitempty"`
	}
	out := struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}{Role: m.Role}

	if len(m.Parts) == 1 && m.Parts[0].ImageURL == "" {
		out.Content = m.Parts[0].Text
		return json.Marshal(out)
	}

	parts := make([]outPart, 0, len(m.Parts))
	for _, p := range m.Parts {
		if p.ImageURL != "" {
			parts = append(parts, outPart{Type: "image_url", ImageURL: map[string]string{"url": p.ImageURL}})
			continue
		}
		if p.Text != "" {
			parts = append(parts, outPart{Type: "text", Text: p.Text})
		}
	}
	out.Content = parts
	return json.Marshal(out)
}

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	Temperature    *float64        `json:"temperature,omitempty"`
	Stream         bool            `json:"stream,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

// Options tweaks a single request.
type Options struct {
	JSONObject  bool
	MaxTokens   int
	Temperature *float64
}

// Complete performs a non-streaming completion and returns the assistant text.
func (c *Client) Complete(ctx context.Context, msgs []Message, opts Options) (string, error) {
	body, err := c.request(ctx, msgs, opts, false)
	if err != nil {
		return "", err
	}
	defer body.Close()

	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("decode response: %w (body: %s)", err, snippet(data))
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return "", fmt.Errorf("upstream error: %s", resp.Error.Message)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no choices in response: %s", snippet(data))
	}
	return resp.Choices[0].Message.Content, nil
}

// Stream performs a streaming completion, invoking onDelta for each chunk, and
// returns the full text.
func (c *Client) Stream(ctx context.Context, msgs []Message, opts Options, onDelta func(string)) (string, error) {
	body, err := c.request(ctx, msgs, opts, true)
	if err != nil {
		return "", err
	}
	defer body.Close()

	var full strings.Builder
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if chunk.Error != nil && chunk.Error.Message != "" {
			return full.String(), fmt.Errorf("upstream error: %s", chunk.Error.Message)
		}
		for _, ch := range chunk.Choices {
			if ch.Delta.Content == "" {
				continue
			}
			full.WriteString(ch.Delta.Content)
			if onDelta != nil {
				onDelta(ch.Delta.Content)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return full.String(), err
	}
	return full.String(), nil
}

func (c *Client) request(ctx context.Context, msgs []Message, opts Options, stream bool) (io.ReadCloser, error) {
	if c.BaseURL == "" {
		return nil, fmt.Errorf("接口地址未配置")
	}
	if c.Model == "" {
		return nil, fmt.Errorf("模型名未配置")
	}

	maxTokens := c.MaxTokens
	if opts.MaxTokens > 0 {
		maxTokens = opts.MaxTokens
	}
	temp := c.Temperature
	if opts.Temperature != nil {
		temp = *opts.Temperature
	}

	req := chatRequest{
		Model:       c.Model,
		Messages:    msgs,
		MaxTokens:   maxTokens,
		Temperature: &temp,
		Stream:      stream,
	}
	if opts.JSONObject {
		req.ResponseFormat = &responseFormat{Type: "json_object"}
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := c.BaseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	if stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		return nil, fmt.Errorf("%s 返回 %s: %s", url, resp.Status, snippet(data))
	}
	return resp.Body, nil
}

func snippet(data []byte) string {
	s := strings.TrimSpace(string(data))
	if len(s) > 500 {
		return s[:500] + "…"
	}
	return s
}
