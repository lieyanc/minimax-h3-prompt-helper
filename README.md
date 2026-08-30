# MiniMax H3 Prompt Helper

A single Go binary that helps you write MiniMax H3 video prompts through an
interactive question flow, driven by your own local ComfyUI workflows and a
vision model that actually looks at your reference images.

It implements the [`h3-prompt-writing`](https://github.com/MiniMax-AI/MiniMax-H3/tree/main/skills/h3-prompt-writing)
skill as a running program rather than a document: the guide is embedded in the
binary, the workflow supplies the hard constraints, and a deterministic
validator checks the result against both.

## Why

The skill is a static writing specification. It assumes the agent already knows
what you want and can see your reference images. This tool fills those two gaps:

- **The vision model looks** — each reference image is read into a fixed JSON
  fact sheet (subjects, environment, lighting, composition, shot size, verbatim
  on-screen text) that pre-fills the answers.
- **The question agent asks** — the guide, the workflow constraints and those
  fact sheets go to a model, which writes the next page of questions itself.
  Two pictures may be a character and a location, two characters, or a location
  and a character; the questions follow what is actually in them instead of a
  fixed list.
- **The workflow constrains** — mode, canvas, frame count and wired reference
  slots are read straight out of the ComfyUI workflow JSON.
- **The validator enforces** — field order, timestamps, label closure, verbatim
  dialogue and language rules are checked in pure Go. No model marks its own
  homework.

## The flow

One page at a time, with everything already fixed kept in the rail on the left:

1. **工作流约束** — the mode, canvas and duration read out of the workflow, with
   the duration adjustable on the model's frame grid.
2. **参考图** — the pictures the workflow loads, plus any you attach here by
   hand, and the vision pass over them.
3. **追问** — the interview, one agent-written page at a time. Each page is
   planned after the previous one is handed in, so an answer can change what
   gets asked next.
4. **生成与校验** — write the prompt, validate it, repair it, and list whatever
   still has to be done in ComfyUI. Reasoning and prompt text are streamed to
   separate live panels when the provider exposes both.

## How the questions are decided

`POST /api/tasks/{id}/plan` sends the embedded skill, the guide for the mode,
the workflow constraints, the vision facts and every answer so far to the
**writing model**, and asks for one page of at most three questions. The model
picks the wording, the options and the order; the server pins down the parts the
rest of the pipeline depends on:

- **Roles.** A question may claim a role (`dialogue`, `shots`, `image_role`,
  `music`, …). That is how the validator finds the verbatim dialogue and how the
  writing prompt finds the shot count, whatever slot key the agent invented.
- **Controlled vocabularies.** A question tagged `vocab: camera` (or `style`,
  `retention`, `audio_retention`, `task_type`, `shots`) has its options replaced
  with the guide's canonical list, so an invented camera move cannot reach the
  prompt.
- **Sanity.** Answered slots are never asked twice, a page is capped at three
  questions, a choice with no options degrades to free text, and a suggestion
  that is not one of the options is dropped.
- **Fallback.** If the endpoint is unreachable or the reply is unusable, the
  built-in fixed slot table asks that page instead and says so on screen. The
  opening intent question is always the fixed one — the agent has nothing to
  reason about before it.

The interview ends when the agent reports it has everything, when you skip the
rest, or after twelve pages.

## Reference images that are not in the workflow

Images can be attached in the browser: dropped in as an extra `<Picture N>`, or
uploaded onto an existing label to stand in for what the workflow currently
loads. They are stored next to the task data — **nothing is ever written into
ComfyUI's folders** — so the last step lists what you still have to do there:

| Case | What the notice says |
| --- | --- |
| an upload standing in for a wired slot | which node to upload it to, and which file the workflow still loads |
| an upload beyond what the workflow wires | to add that reference input in ComfyUI and connect it |
| a workflow image missing from `input/` | to upload it again in ComfyUI |

Swapping the picture behind a label drops the vision facts and the answers that
were about it, because they described a different image; the agent asks about
the new one.

## What it reads from a ComfyUI workflow

For every `MiniMaxH3ImageToVideo` / `MiniMaxH3ReferenceToVideo` /
`EmptyMiniMaxH3LatentAV` node it finds, including bypassed branches:

| Read | How |
| --- | --- |
| Input mode (T2VA / I2VA / FL2VA / L2VA / Ref2VA) | which of `first_frame` / `last_frame` are linked, or the presence of a reference node |
| Canvas size | resolves the `width` / `height` inputs through `ResolutionSelector`, primitives and switches |
| Frame count and effective duration | resolves `length`, evaluating `ComfyMathExpression` with Python modulo semantics, then snaps to the model's 17k+5 grid at 24 fps |
| Reference slots | counts wired `ref_image_N` / `ref_video_N` / `ref_audio_N` inputs and assigns `<Picture N>` / `<Video N>` / `<Audio N>` labels |
| Source images | traces each image link back through resizers and reroutes to its `LoadImage` filename, then serves the file from the ComfyUI `input` directory |
| Active branch | node `mode` (0 = live, 2/4 = muted/bypassed) |

Because the server runs on the same machine as ComfyUI, the workflow's own
reference images never need to be uploaded — they are read from `ComfyUI/input`
and shown inline. Anything you attach by hand lives with the task data instead.

## What the validator checks

Deterministic, no tokens spent:

- section names, presence and order (three fields for base modes, six for Ref2VA)
- the first-line alignment instruction for I2VA / FL2VA / L2VA, including the
  two-decimal duration
- `[Shot 1]` carries no timestamp; later shots have strictly increasing
  `At MM:SS.mmm` cut times below the workflow duration
- reference labels are declared, contiguous, closed, and limited to what the
  workflow actually wires
- `<d>` blocks balance, carry a `[Language]` tag, and reproduce user dialogue
  verbatim (a diff, so a paraphrase fails)
- voice-over lines are followed by the required closed-lips statement
- speaker IDs are contiguous and never appear in `retention_analysis`
- Ref2VA retention markers and summary task types come from their fixed
  vocabularies
- English-only body text, with `<d>` content and double-quoted screen text
  exempt (blocking or advisory, configurable)
- abstract quality words the guide forbids

Blocking findings are fed back to the writing model automatically, up to a
configurable number of repair rounds.

## Requirements

- Go 1.26+ and Node 20+ to build
- A running ComfyUI installation on the same machine (only its files are read)
- Any OpenAI-compatible `/chat/completions` endpoint with vision support

## Build and run

```bash
make deps     # once: install frontend dependencies
make build    # builds web/ into webui/dist and links the single binary
make run      # rebuilds both frontend and backend, then starts the server
./bin/h3helper
```

Useful flags:

```bash
./bin/h3helper -listen 0.0.0.0:8199        # bind address (LAN by default)
./bin/h3helper -comfyui /path/to/ComfyUI   # ComfyUI root
./bin/h3helper -data /path/to/data          # override the task data directory
```

The server prints every URL that reaches it, so you can open it from another
machine on the LAN the same way you open ComfyUI itself.

For frontend work, `make dev` runs the Go server and the Vite dev server
together; Vite proxies `/api` to `127.0.0.1:8199`.

## Storage

No database. The configuration is stored in the directory where the program is
run, while task data is kept under its `data/` directory by default:

```
./
  config.json          providers, models, ComfyUI paths, validation settings
  data/
    tasks/<id>.json    one file per prompt-writing session
    uploads/<id>/      reference images attached by hand in that session
```

Pass `-data /path/to/data` to use another directory explicitly.

Each task file keeps the full history: constraints, vision facts, every question
the agent wrote and the answers given, every generation attempt and its
findings.

## Configuration

Endpoints come in two layers. A **provider** is one OpenAI-compatible address
plus the key it needs; a **model** is what goes into the `model` field, which
provider serves it, and its own sampling. The two roles then point at a model by
id:

```json
{
  "providers": [
    { "id": "openai", "name": "OpenAI",
      "baseURL": "https://api.openai.com/v1", "apiKey": "" }
  ],
  "models": [
    { "id": "vision", "providerId": "openai", "model": "gpt-4o",
      "displayName": "看图的模型", "maxTokens": 4096, "temperature": 0.2,
      "reasoningEffort": "" },
    { "id": "writer", "providerId": "openai", "model": "gpt-4o",
      "displayName": "写提示词的模型", "maxTokens": 8192, "temperature": 0.7,
      "reasoningEffort": "" }
  ],
  "vision": { "modelId": "vision", "imageMaxEdge": 1280 },
  "writer": { "modelId": "writer" }
}
```

Two entries on one model is the default, because the roles want opposite
settings: the vision pass fills a fact sheet and should not improvise, the
writing pass produces long text and needs the token budget for its repair
rounds. Split them across providers when the model that can see is not the model
that writes best — a local VLM for the images, a stronger remote one for the
prompt. The question agent runs on the writing model, since it reads the same
guide.

`reasoningEffort` is optional. An empty string leaves the provider default and
does not send `reasoning_effort`; the settings page also offers `none`,
`minimal`, `low`, `medium`, `high`, and `xhigh`. When it is set, `temperature`
is omitted and the token limit is sent as `max_completion_tokens`, as required
by OpenAI reasoning models. During prompt generation the server forwards
content chunks immediately, and recognises the common `reasoning_content`,
`reasoning`, `thinking`, and `reasoning_details` stream fields for the separate
live reasoning panel.

A file written before providers existed, where `vision` and `writer` each
carried their own `baseURL`, `apiKey` and `model`, is migrated on the next
start: the endpoints become providers, both sampling profiles survive as
separate model entries, and the file is rewritten in place.

The defaults are compiled in (`config.Default`) and that is the only place they
live. On startup:

- no `config.json` → the full default file is written out, every key present, so
  it can be hand-edited without guessing the schema;
- an existing file → it is merged over the defaults, so a file from an older
  version or one trimmed down to two keys still starts with every field set. Any
  key that had to be filled in is rewritten to disk and listed in the startup log.

Values that cannot work are repaired the same way: an empty `listen`, a
non-positive `maxTokens`, a `baseURL` with `/chat/completions` pasted onto the
end, a duplicate or missing id, or a reference to something that was deleted — a
model whose provider is gone falls back to the first one, and a role whose model
is gone follows suit, rather than failing at request time. Zeros that mean
something are left alone — `maxRepairRounds: 0` disables the repair loop,
`imageMaxEdge: 0` uploads the original image.

The settings page also offers a few compiled-in endpoint presets (OpenAI,
DashScope, Zhipu, SiliconFlow, OpenRouter, Ollama, LM Studio) that fill in a new
provider's address and suggest a model name. They are constants, never stored in
the config file.

API keys are only ever written to `config.json` (mode 0600). `GET /api/config`
replaces each provider's key with a `hasKey` boolean, and an empty `apiKey` in a
`PUT` means "leave the stored one alone".

## Access control

The server binds `0.0.0.0` so it is reachable from the LAN. Set `token` in
`config.json` and restart to require a shared secret; the browser then stores it
in `localStorage` and sends it as `X-Auth-Token`. Without a token, anyone on the
network can open it.

## Interface

Dark by default, built around `#018eee`. The theme is resolved before first
paint, so opening it over the LAN does not flash white. Press `d` outside a text
field for the light palette; the choice is stored per browser. Every text pair in
both palettes clears WCAG AA.

## Layout

```
main.go                  flags, config load, HTTP server
internal/comfy/          workflow parsing, value resolution, input directory
internal/skill/          the embedded h3-prompt-writing guide
internal/questions/      the question agent: guide + facts → the next page
internal/slots/          the fixed question table, used as the fallback
internal/vision/         image → structured facts
internal/promptgen/      guide + constraints + answers → model messages
internal/validate/       deterministic format checks
internal/store/          JSON task store
internal/api/            HTTP handlers, uploads and SSE streams
webui/                   embedded frontend build output
web/                     React + Tailwind + shadcn/ui source
```

## Credits

The prompt format guide in `internal/skill/assets/` is copied from
[MiniMax-AI/MiniMax-H3](https://github.com/MiniMax-AI/MiniMax-H3) under that
project's license.
