// Package vision turns a reference image into structured facts that can be
// dropped straight into slot suggestions and subject definitions. The vision
// model never chats; it only fills a fixed JSON shape.
package vision

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"h3helper/internal/llm"
	"h3helper/internal/task"
)

const systemPrompt = `You analyse a single reference image that will be used for MiniMax H3 video generation.
Report only what is actually visible. Never invent details you cannot see.
Reply with a single JSON object and nothing else — no markdown fences, no commentary.

Schema:
{
  "style": "one of: Cinematic, live-action, 2D-animated, 3D CG, claymation, watercolor, vintage film (pick the closest)",
  "summary": "one English sentence describing the whole frame",
  "subjects": [
    {"name": "short label, e.g. young woman", "kind": "person|animal|object|environment", "appearance": "concrete visual details: hair, clothing, colours, materials", "position": "where it sits in the frame"}
  ],
  "environment": "the setting and notable props",
  "lighting": "light direction, quality and colour temperature",
  "composition": "framing and layout",
  "shotSize": "one of: extreme close-up, close-up, medium close-up, medium shot, medium-wide shot, wide shot, extreme wide shot",
  "visibleText": ["any text visible in the image, transcribed verbatim in its original language and script"],
  "colorPalette": "dominant colours",
  "possibleAction": "one English sentence describing the most plausible action that could start from this frame"
}

Rules:
- Write every field in English except visibleText, which keeps the original language and characters.
- Prefer concrete nouns and materials over words like beautiful, cinematic or atmospheric.
- Return an empty array for visibleText when the image contains no readable text.`

// Analyze reads one image and returns structured facts.
func Analyze(ctx context.Context, client *llm.Client, imagePath, label string, maxEdge int) (task.Facts, error) {
	facts := task.Facts{Label: label, AnalyzedAt: time.Now()}

	dataURL, err := llm.DataURL(imagePath, maxEdge)
	if err != nil {
		return facts, fmt.Errorf("读取图片失败: %w", err)
	}

	msgs := []llm.Message{
		llm.Text("system", systemPrompt),
		{Role: "user", Parts: []llm.Part{
			{Text: fmt.Sprintf("Analyse this reference image (it will be referred to as %s in the prompt).", label)},
			{ImageURL: dataURL},
		}},
	}

	raw, err := client.Complete(ctx, msgs, llm.Options{JSONObject: true})
	if err != nil {
		// Some compatible servers reject response_format; retry without it.
		raw, err = client.Complete(ctx, msgs, llm.Options{})
		if err != nil {
			return facts, err
		}
	}

	if err := parseFacts(raw, &facts); err != nil {
		return facts, err
	}
	return facts, nil
}

func parseFacts(raw string, facts *task.Facts) error {
	body := llm.ExtractJSON(raw)
	if body == "" {
		return fmt.Errorf("模型没有返回 JSON：%s", truncate(raw, 200))
	}
	var parsed struct {
		Style          string             `json:"style"`
		Summary        string             `json:"summary"`
		Subjects       []task.FactSubject `json:"subjects"`
		Environment    string             `json:"environment"`
		Lighting       string             `json:"lighting"`
		Composition    string             `json:"composition"`
		ShotSize       string             `json:"shotSize"`
		VisibleText    []string           `json:"visibleText"`
		ColorPalette   string             `json:"colorPalette"`
		PossibleAction string             `json:"possibleAction"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		return fmt.Errorf("解析视觉分析结果失败: %w", err)
	}
	facts.Style = parsed.Style
	facts.Summary = parsed.Summary
	facts.Subjects = parsed.Subjects
	facts.Environment = parsed.Environment
	facts.Lighting = parsed.Lighting
	facts.Composition = parsed.Composition
	facts.ShotSize = parsed.ShotSize
	facts.VisibleText = parsed.VisibleText
	facts.ColorPalette = parsed.ColorPalette
	facts.PossibleAction = parsed.PossibleAction
	return nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
