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
- **The question flow asks** — a fixed slot table derived from the spec asks
  only what the model cannot infer: the action path, the verbatim dialogue, the
  camera move, the ending state. Three questions per round, typically 3–5
  rounds.
- **The workflow constrains** — mode, canvas, frame count and wired reference
  slots are read straight out of the ComfyUI workflow JSON.
- **The validator enforces** — field order, timestamps, label closure, verbatim
  dialogue and language rules are checked in pure Go. No model marks its own
  homework.

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

Because the server runs on the same machine as ComfyUI, reference images never
need to be uploaded — they are read from `ComfyUI/input` and shown inline.

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
./bin/h3helper -data ~/.local/share/h3-prompt-helper
```

The server prints every URL that reaches it, so you can open it from another
machine on the LAN the same way you open ComfyUI itself.

For frontend work, `make dev` runs the Go server and the Vite dev server
together; Vite proxies `/api` to `127.0.0.1:8199`.

## Storage

No database. Everything is JSON under the data directory:

```
~/.local/share/h3-prompt-helper/
  config.json          endpoints, keys, ComfyUI paths, validation settings
  tasks/<id>.json      one file per prompt-writing session
```

Each task file keeps the full history: constraints, vision facts, every question
round with its answers, every generation attempt and its findings.

## Configuration

The defaults are compiled in (`config.Default`) and that is the only place they
live. On startup:

- no `config.json` → the full default file is written out, every key present, so
  it can be hand-edited without guessing the schema;
- an existing file → it is merged over the defaults, so a file from an older
  version or one trimmed down to two keys still starts with every field set. Any
  key that had to be filled in is rewritten to disk and listed in the startup log.

Values that cannot work are repaired the same way: an empty `listen`, a
non-positive `maxTokens`, or a `baseURL` with `/chat/completions` pasted onto the
end. Zeros that mean something are left alone — `maxRepairRounds: 0` disables the
repair loop, `imageMaxEdge: 0` uploads the original image.

The settings page also offers a few compiled-in endpoint presets (OpenAI,
DashScope, Zhipu, SiliconFlow, OpenRouter, Ollama, LM Studio) that fill in the
address and a plausible vision model name. They are constants, never stored in
the config file.

API keys are only ever written to `config.json` (mode 0600). `GET /api/config`
returns `visionHasKey`/`writerHasKey` booleans instead of the keys themselves,
and an empty `apiKey` in a `PUT` means "leave it unchanged".

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
internal/slots/          the question engine
internal/vision/         image → structured facts
internal/promptgen/      guide + constraints + answers → model messages
internal/validate/       deterministic format checks
internal/store/          JSON task store
internal/api/            HTTP handlers and SSE streams
webui/                   embedded frontend build output
web/                     React + Tailwind + shadcn/ui source
```

## Credits

The prompt format guide in `internal/skill/assets/` is copied from
[MiniMax-AI/MiniMax-H3](https://github.com/MiniMax-AI/MiniMax-H3) under that
project's license.
