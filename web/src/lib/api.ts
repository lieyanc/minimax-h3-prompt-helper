/**
 * API client for the Go backend. Everything is plain fetch; SSE endpoints are
 * read from the response stream so the auth header can be attached (EventSource
 * cannot set headers).
 */

const TOKEN_KEY = "h3helper.token"

export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) ?? ""
}

export function setToken(token: string) {
  if (token) localStorage.setItem(TOKEN_KEY, token)
  else localStorage.removeItem(TOKEN_KEY)
}

export class ApiError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.status = status
  }
}

function headers(extra?: Record<string, string>): Record<string, string> {
  const h: Record<string, string> = { ...extra }
  const token = getToken()
  if (token) h["X-Auth-Token"] = token
  return h
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, {
    ...init,
    headers: headers({
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
      ...((init?.headers as Record<string, string>) ?? {}),
    }),
  })
  const text = await res.text()
  let data: unknown = null
  if (text) {
    try {
      data = JSON.parse(text)
    } catch {
      data = { error: text }
    }
  }
  if (!res.ok) {
    const msg =
      (data as { error?: string } | null)?.error ?? `请求失败 (${res.status})`
    throw new ApiError(msg, res.status)
  }
  return data as T
}

export type SseEvent = { event: string; data: unknown }

/** Streams an SSE endpoint, invoking onEvent for each parsed event. */
export async function streamPost(
  path: string,
  body: unknown,
  onEvent: (e: SseEvent) => void,
  signal?: AbortSignal
): Promise<void> {
  const res = await fetch(path, {
    method: "POST",
    signal,
    headers: headers({ "Content-Type": "application/json" }),
    body: JSON.stringify(body ?? {}),
  })
  if (!res.ok) {
    const text = await res.text()
    let msg = `请求失败 (${res.status})`
    try {
      msg = (JSON.parse(text) as { error?: string }).error ?? msg
    } catch {
      if (text) msg = text
    }
    throw new ApiError(msg, res.status)
  }
  if (!res.body) throw new ApiError("服务端没有返回流", 500)

  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ""

  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    buffer += decoder.decode(value, { stream: true })

    let idx: number
    while ((idx = buffer.indexOf("\n\n")) >= 0) {
      const chunk = buffer.slice(0, idx)
      buffer = buffer.slice(idx + 2)
      let event = "message"
      const dataLines: string[] = []
      for (const line of chunk.split("\n")) {
        if (line.startsWith("event:")) event = line.slice(6).trim()
        else if (line.startsWith("data:")) dataLines.push(line.slice(5).trim())
      }
      if (!dataLines.length) continue
      try {
        onEvent({ event, data: JSON.parse(dataLines.join("\n")) })
      } catch {
        /* ignore malformed frames */
      }
    }
  }
}

// ------------------------------------------------------------------ types --

export type RefSlot = {
  slot: string
  kind: "image" | "video" | "audio"
  role: string
  connected: boolean
  source: string
  sourceExists: boolean
  label: string
}

export type Variant = {
  id: string
  mode: string
  nodeId: number
  nodeType: string
  title: string
  active: boolean
  width: number
  height: number
  frames: number
  fps: number
  duration: number
  framesOnGrid: boolean
  derived: boolean
  images: RefSlot[]
  videos: RefSlot[]
  audios: RefSlot[]
  guides: number
  promptText: string
  promptNodeId: number
  notes: string[]
}

export type Workflow = {
  file: string
  name: string
  dir: string
  modTime: string
  size: number
  variants: Variant[]
  models: { unet: string[]; lora: string[]; clip: string[]; steps: number[] }
  error?: string
}

export type Option = { value: string; label: string; desc?: string }

export type Question = {
  slot: string
  group: string
  title: string
  help?: string
  kind: "text" | "textarea" | "choice" | "multichoice"
  options?: Option[]
  suggestion?: string
  placeholder?: string
  required: boolean
  allowFree?: boolean
}

export type Finding = {
  rule: string
  severity: "error" | "warn"
  message: string
  detail?: string
  line?: number
}

export type FactSubject = {
  name: string
  kind: string
  appearance: string
  position: string
}

export type Facts = {
  label: string
  style: string
  summary: string
  subjects: FactSubject[]
  environment: string
  lighting: string
  composition: string
  shotSize: string
  visibleText: string[]
  colorPalette: string
  possibleAction: string
  error?: string
  analyzedAt: string
}

export type TaskImage = {
  label: string
  slot: string
  role: string
  source: string
  origin: string
  missing: boolean
}

export type Constraints = {
  mode: string
  width: number
  height: number
  frames: number
  fps: number
  duration: number
  aspectLabel: string
  pictureLabels: string[]
  videoLabels: string[]
  audioLabels: string[]
  maxShots: number
  notes: string[]
}

export type Attempt = {
  index: number
  prompt: string
  findings: Finding[]
  repaired: boolean
  at: string
}

export type Task = {
  id: string
  title: string
  status: string
  createdAt: string
  updatedAt: string
  workflowFile: string
  workflowName: string
  variantId: string
  variantNode: number
  constraints: Constraints
  images: TaskImage[]
  facts: Record<string, Facts>
  brief: string
  answers: Record<string, string>
  pending: Question[]
  prompt: string
  findings: Finding[]
  attempts: Attempt[]
  generatedAt: string
  error?: string

  // view extras
  questions: Question[]
  missing: string[]
  answeredCount: number
  requiredCount: number
  blockingFindings: number
}

export type TaskSummary = {
  id: string
  title: string
  status: string
  mode: string
  workflowName: string
  duration: number
  images: number
  answered: number
  findings: number
  hasPrompt: boolean
  createdAt: string
  updatedAt: string
}

export type Config = {
  listen: string
  token: string
  comfyuiRoot: string
  workflowDirs: string[] | null
  vision: {
    baseURL: string
    apiKey: string
    model: string
    maxTokens: number
    temperature: number
    imageMaxEdge: number
  }
  writer: {
    sameAsVision: boolean
    baseURL: string
    apiKey: string
    model: string
    maxTokens: number
    temperature: number
  }
  strictEnglish: boolean
  maxRepairRounds: number
  visionHasKey: boolean
  writerHasKey: boolean
  searchDirs: string[]
  inputDir: string
  presets: Preset[]
  configPath: string
}

/** A built-in endpoint the settings page can fill in with one click. */
export type Preset = {
  name: string
  baseURL: string
  model: string
  note?: string
}

export type InputImage = { name: string; size: number; modTime: string }

// --------------------------------------------------------------- endpoints --

export const api = {
  health: () =>
    request<{
      ok: boolean
      version: string
      needsToken: boolean
      comfyuiRoot: string
    }>("/api/health"),

  getConfig: () => request<Config>("/api/config"),

  saveConfig: (patch: unknown) =>
    request<Config>("/api/config", {
      method: "PUT",
      body: JSON.stringify(patch),
    }),

  testConfig: (target: "vision" | "writer") =>
    request<{ ok: boolean; reply?: string; model?: string; error?: string }>(
      `/api/config/test?target=${target}`,
      { method: "POST" }
    ),

  workflows: () =>
    request<{ dirs: string[]; inputDir: string; workflows: Workflow[] }>(
      "/api/workflows"
    ),

  inputs: () =>
    request<{ dir: string; images: InputImage[] }>("/api/inputs"),

  tasks: () => request<{ tasks: TaskSummary[] }>("/api/tasks"),

  createTask: (workflowFile: string, variantId: string, title?: string) =>
    request<Task>("/api/tasks", {
      method: "POST",
      body: JSON.stringify({ workflowFile, variantId, title }),
    }),

  task: (id: string) => request<Task>(`/api/tasks/${id}`),

  deleteTask: (id: string) =>
    request<{ ok: boolean }>(`/api/tasks/${id}`, { method: "DELETE" }),

  patchTask: (id: string, patch: unknown) =>
    request<Task>(`/api/tasks/${id}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    }),

  revalidate: (id: string) =>
    request<Task>(`/api/tasks/${id}/validate`, { method: "POST" }),
}

/** URL for a ComfyUI input image, with the token attached when needed. */
export function imageURL(name: string): string {
  const token = getToken()
  const q = new URLSearchParams({ name })
  if (token) q.set("token", token)
  return `/api/image?${q.toString()}`
}
