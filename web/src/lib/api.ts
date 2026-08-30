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
  // Only JSON bodies get a content type: the browser has to set its own
  // multipart boundary for FormData uploads.
  const isJSON = typeof init?.body === "string"
  const res = await fetch(path, {
    ...init,
    headers: headers({
      ...(isJSON ? { "Content-Type": "application/json" } : {}),
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
  /** How the answer is consumed downstream; empty when it only feeds the brief. */
  role?: string
  /** The reference label the question is about, e.g. "&lt;Picture 2&gt;". */
  label?: string
  /** Why the H3 format needs this answered. */
  why?: string
  enLabel?: string
  vocab?: string
}

/** One screen of questions, as the question agent handed it over. */
export type Page = {
  index: number
  title: string
  intro?: string
  questions: Question[]
  remaining: number
  done: boolean
  note?: string
  fallback?: boolean
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
  origin: "workflow" | "upload" | string
  missing: boolean
  /** Stored file name, for images uploaded by hand in this tool. */
  file?: string
  /** Whether the ComfyUI workflow actually feeds this label. */
  wired: boolean
  /** The workflow file this upload stands in for. */
  replaces?: string
}

/** Something the user still has to do inside ComfyUI itself. */
export type ComfyNotice = {
  label: string
  slot: string
  file: string
  kind: "replace" | "add" | "missing"
  text: string
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

export type Revision = {
  index: number
  request: string
  previousPrompt?: string
  prompt: string
  findings: Finding[]
  at: string
}

export type Step = "constraints" | "images" | "questions" | "generate"

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
  step: Step
  asked: Question[]
  plan: Page
  planError?: string
  prompt: string
  findings: Finding[]
  attempts: Attempt[]
  revisions: Revision[]
  generatedAt: string
  error?: string

  // view extras
  questions: Question[]
  missing: string[]
  /** Reference images that must be successfully analysed before step three. */
  visionPending: string[]
  answeredCount: number
  requiredCount: number
  blockingFindings: number
  comfyNotices: ComfyNotice[]
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

/** One OpenAI-compatible endpoint. Models point at it by id. */
export type Provider = {
  id: string
  name: string
  baseURL: string
  /** The server never sends keys back, only whether one is stored. */
  hasKey: boolean
  /** Set while editing; empty on save means "leave the stored key alone". */
  apiKey?: string
}

/** One model served by a provider. */
export type ModelEntry = {
  id: string
  providerId: string
  model: string
  displayName: string
  maxTokens: number
  temperature: number
  /** OpenAI-compatible reasoning_effort; empty means do not send the field. */
  reasoningEffort: string
}

export type Config = {
  listen: string
  token: string
  comfyuiRoot: string
  workflowDirs: string[] | null
  providers: Provider[] | null
  models: ModelEntry[] | null
  vision: {
    modelId: string
    imageMaxEdge: number
  }
  question: {
    modelId: string
  }
  writer: {
    modelId: string
    imageMaxEdge: number
  }
  strictEnglish: boolean
  maxRepairRounds: number
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

export type TestResult = {
  ok: boolean
  reply?: string
  model?: string
  label?: string
  provider?: string
  error?: string
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

  /** Tests one model, by id or by the role it is assigned to. */
  testConfig: (target: {
    modelId?: string
    role?: "vision" | "question" | "writer"
  }) => {
    const q = new URLSearchParams()
    if (target.modelId) q.set("modelId", target.modelId)
    if (target.role) q.set("target", target.role)
    return request<TestResult>(`/api/config/test?${q.toString()}`, {
      method: "POST",
    })
  },

  workflows: () =>
    request<{ dirs: string[]; inputDir: string; workflows: Workflow[] }>(
      "/api/workflows"
    ),

  inputs: () => request<{ dir: string; images: InputImage[] }>("/api/inputs"),

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

  /** Asks the question agent for the next page. Pass force to replace one. */
  plan: (id: string, force = false) =>
    request<Task>(`/api/tasks/${id}/plan${force ? "?force=1" : ""}`, {
      method: "POST",
    }),

  /**
   * Uploads a reference image. With a label it stands in for that reference;
   * without one it is added as a new label the workflow does not wire yet.
   */
  uploadImage: (id: string, file: File, label?: string) => {
    const body = new FormData()
    body.append("file", file)
    if (label) body.append("label", label)
    return request<Task>(`/api/tasks/${id}/images`, { method: "POST", body })
  },

  deleteImage: (id: string, label: string) =>
    request<Task>(`/api/tasks/${id}/images/${encodeURIComponent(label)}`, {
      method: "DELETE",
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

/** URL for one attached reference image, wherever it is stored. */
export function taskImageURL(taskId: string, img: TaskImage): string {
  if (img.origin === "upload" && img.file) {
    const token = getToken()
    const q = new URLSearchParams()
    if (token) q.set("token", token)
    const query = q.toString()
    return `/api/tasks/${taskId}/uploads/${encodeURIComponent(img.file)}${
      query ? `?${query}` : ""
    }`
  }
  return imageURL(img.source)
}
