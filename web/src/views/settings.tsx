import { useEffect, useState } from "react"
import { PlugZapIcon, PlusIcon, SaveIcon, Trash2Icon } from "lucide-react"
import { toast } from "sonner"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import {
  api,
  getToken,
  setToken,
  type Config,
  type ModelEntry,
  type Preset,
  type Provider,
} from "@/lib/api"

// Offered alongside the presets when the endpoint is not one of them.
const BLANK_PRESET = "自定义"
const DEFAULT_REASONING = "__provider_default__"

const REASONING_EFFORTS = [
  { value: DEFAULT_REASONING, label: "供应商默认（不发送）" },
  { value: "none", label: "none" },
  { value: "minimal", label: "minimal" },
  { value: "low", label: "low" },
  { value: "medium", label: "medium" },
  { value: "high", label: "high" },
  { value: "xhigh", label: "xhigh" },
]

/** The label a model is shown by, falling back to what the API is sent. */
const labelOf = (m: ModelEntry) => m.displayName || m.model || m.id

/** Turns an endpoint into an id candidate: host, letters and digits only. */
function slugify(s: string): string {
  return s
    .replace(/^https?:\/\//, "")
    .split("/")[0]
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
}

function uniqueId(base: string, taken: string[]): string {
  const stem = base || "item"
  let id = stem
  for (let n = 2; taken.includes(id); n++) id = `${stem}-${n}`
  return id
}

export function SettingsView() {
  const [cfg, setCfg] = useState<Config | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState("")
  const [dirs, setDirs] = useState("")
  const [browserToken, setBrowserToken] = useState(getToken())

  useEffect(() => {
    void (async () => {
      try {
        const c = await api.getConfig()
        setCfg(c)
        setDirs((c.workflowDirs ?? []).join("\n"))
      } catch (e) {
        toast.error((e as Error).message)
      } finally {
        setLoading(false)
      }
    })()
  }, [])

  if (loading || !cfg) return <Skeleton className="h-96 w-full rounded-xl" />

  const providers = cfg.providers ?? []
  const models = cfg.models ?? []
  const presets = cfg.presets ?? []

  const patch = <K extends keyof Config>(key: K, value: Config[K]) =>
    setCfg({ ...cfg, [key]: value })

  // ---------------------------------------------------------- providers --

  const updateProvider = (index: number, next: Provider) => {
    const prev = providers[index]
    setCfg({
      ...cfg,
      providers: providers.map((p, i) => (i === index ? next : p)),
      // Renaming an id must not orphan the models that referred to it.
      models:
        prev.id === next.id
          ? models
          : models.map((m) =>
              m.providerId === prev.id ? { ...m, providerId: next.id } : m
            ),
    })
  }

  const addProvider = (preset?: Preset) => {
    const id = uniqueId(
      preset ? slugify(preset.baseURL) : "provider",
      providers.map((p) => p.id)
    )
    setCfg({
      ...cfg,
      providers: [
        ...providers,
        {
          id,
          name: preset?.name ?? "",
          baseURL: preset?.baseURL ?? "",
          hasKey: false,
        },
      ],
    })
  }

  const removeProvider = (index: number) => {
    if (providers.length <= 1) {
      toast.error("至少要留一个供应商")
      return
    }
    const gone = providers[index]
    const rest = providers.filter((_, i) => i !== index)
    const orphans = models.filter((m) => m.providerId === gone.id)
    setCfg({
      ...cfg,
      providers: rest,
      models: models.map((m) =>
        m.providerId === gone.id ? { ...m, providerId: rest[0].id } : m
      ),
    })
    if (orphans.length > 0) {
      toast.info(
        `${orphans.length} 个模型原来挂在这里，已改到「${rest[0].name || rest[0].id}」`
      )
    }
  }

  // ------------------------------------------------------------- models --

  const updateModel = (index: number, next: ModelEntry) => {
    const prev = models[index]
    const renamed = prev.id !== next.id
    setCfg({
      ...cfg,
      models: models.map((m, i) => (i === index ? next : m)),
      vision:
        renamed && cfg.vision.modelId === prev.id
          ? { ...cfg.vision, modelId: next.id }
          : cfg.vision,
      question:
        renamed && cfg.question.modelId === prev.id
          ? { modelId: next.id }
          : cfg.question,
      writer:
        renamed && cfg.writer.modelId === prev.id
          ? { ...cfg.writer, modelId: next.id }
          : cfg.writer,
    })
  }

  const addModel = () => {
    const id = uniqueId(
      "model",
      models.map((m) => m.id)
    )
    setCfg({
      ...cfg,
      models: [
        ...models,
        {
          id,
          providerId: providers[0]?.id ?? "",
          model: "",
          displayName: "",
          maxTokens: 4096,
          temperature: 0.7,
          reasoningEffort: "",
        },
      ],
    })
  }

  const removeModel = (index: number) => {
    if (models.length <= 1) {
      toast.error("至少要留一个模型")
      return
    }
    const gone = models[index]
    const rest = models.filter((_, i) => i !== index)
    setCfg({
      ...cfg,
      models: rest,
      vision:
        cfg.vision.modelId === gone.id
          ? { ...cfg.vision, modelId: rest[0].id }
          : cfg.vision,
      question:
        cfg.question.modelId === gone.id
          ? { modelId: rest[0].id }
          : cfg.question,
      writer:
        cfg.writer.modelId === gone.id
          ? { ...cfg.writer, modelId: rest[0].id }
          : cfg.writer,
    })
  }

  // -------------------------------------------------------- save & test --

  const persist = async (opts?: { silent?: boolean }) => {
    setSaving(true)
    try {
      const next = await api.saveConfig({
        comfyuiRoot: cfg.comfyuiRoot,
        workflowDirs: dirs
          .split("\n")
          .map((s) => s.trim())
          .filter(Boolean),
        strictEnglish: cfg.strictEnglish,
        maxRepairRounds: cfg.maxRepairRounds,
        providers: providers.map((p) => ({
          id: p.id,
          name: p.name,
          baseURL: p.baseURL,
          // An absent key leaves the stored one alone.
          ...(p.apiKey ? { apiKey: p.apiKey } : {}),
        })),
        models,
        vision: {
          modelId: cfg.vision.modelId,
          imageMaxEdge: cfg.vision.imageMaxEdge,
        },
        question: { modelId: cfg.question.modelId },
        writer: {
          modelId: cfg.writer.modelId,
          imageMaxEdge: cfg.writer.imageMaxEdge,
        },
      })
      setCfg(next)
      if (!opts?.silent) toast.success("配置已保存")
      return true
    } catch (e) {
      toast.error((e as Error).message)
      return false
    } finally {
      setSaving(false)
    }
  }

  // The server tests what is on disk, so an edit has to be saved first.
  const test = async (modelId: string) => {
    setTesting(modelId)
    try {
      if (!(await persist({ silent: true }))) return
      const res = await api.testConfig({ modelId })
      if (res.ok) {
        toast.success(
          `${res.label ?? modelId} 连通，${res.model} 回了：${res.reply}`
        )
      } else {
        toast.error(res.error ?? "连接失败")
      }
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setTesting("")
    }
  }

  return (
    <div className="flex max-w-3xl flex-col gap-6">
      <div className="flex flex-col gap-1">
        <h1 className="text-lg font-semibold">设置</h1>
        <p className="text-sm text-muted-foreground">
          全部写在 <span className="font-mono">{cfg.configPath}</span>{" "}
          里，没有数据库。直接改这个文件也行，缺的键启动时会自动补回默认值。
        </p>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>ComfyUI</CardTitle>
          <CardDescription>
            服务和 ComfyUI 跑在同一台机器上，所以参考图能直接从 input 目录读取。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="comfyui-root">ComfyUI 根目录</FieldLabel>
              <Input
                id="comfyui-root"
                value={cfg.comfyuiRoot}
                onChange={(e) => patch("comfyuiRoot", e.target.value)}
              />
              <FieldDescription>
                input 目录：<span className="font-mono">{cfg.inputDir}</span>
              </FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="dirs">
                额外的工作流目录（每行一个，留空用默认）
              </FieldLabel>
              <Textarea
                id="dirs"
                rows={3}
                value={dirs}
                className="font-mono text-xs"
                onChange={(e) => setDirs(e.target.value)}
              />
              <FieldDescription>
                当前扫描：{cfg.searchDirs.join("  ·  ")}
              </FieldDescription>
            </Field>
          </FieldGroup>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>供应商</CardTitle>
          <CardDescription>
            一个供应商就是一个 OpenAI 兼容的接口加它的 Key。下面的模型按 id
            挂到这里。
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {providers.map((p, i) => (
            <div key={i} className="flex flex-col gap-4 rounded-lg border p-4">
              <div className="flex items-center gap-2">
                <Input
                  aria-label="供应商名称"
                  className="font-medium"
                  value={p.name}
                  placeholder="名称，例如 OpenAI"
                  onChange={(e) =>
                    updateProvider(i, { ...p, name: e.target.value })
                  }
                />
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label="删除供应商"
                  onClick={() => removeProvider(i)}
                >
                  <Trash2Icon />
                </Button>
              </div>
              <FieldGroup>
                <Field orientation="responsive">
                  <Field>
                    <FieldLabel htmlFor={`p-${i}-id`}>id</FieldLabel>
                    <Input
                      id={`p-${i}-id`}
                      className="font-mono"
                      value={p.id}
                      onChange={(e) =>
                        updateProvider(i, { ...p, id: e.target.value })
                      }
                    />
                  </Field>
                  <Field>
                    <FieldLabel htmlFor={`p-${i}-base`}>接口地址</FieldLabel>
                    <Input
                      id={`p-${i}-base`}
                      value={p.baseURL}
                      placeholder="https://api.example.com/v1"
                      onChange={(e) =>
                        updateProvider(i, { ...p, baseURL: e.target.value })
                      }
                    />
                  </Field>
                </Field>
                <Field>
                  <FieldLabel htmlFor={`p-${i}-key`}>API Key</FieldLabel>
                  <Input
                    id={`p-${i}-key`}
                    type="password"
                    value={p.apiKey ?? ""}
                    placeholder={p.hasKey ? "已保存，留空则不修改" : "未设置"}
                    onChange={(e) =>
                      updateProvider(i, { ...p, apiKey: e.target.value })
                    }
                  />
                  <FieldDescription>
                    地址不要带 /chat/completions，程序会自己拼。
                  </FieldDescription>
                </Field>
              </FieldGroup>
            </div>
          ))}

          <div className="flex flex-wrap items-center gap-3">
            <Select
              // Remounting after each pick brings the placeholder back.
              key={providers.length}
              onValueChange={(value) => {
                if (value === BLANK_PRESET) {
                  addProvider()
                  return
                }
                addProvider(presets.find((p) => p.name === value))
              }}
            >
              <SelectTrigger className="w-64">
                <SelectValue placeholder="添加供应商…" />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  <SelectItem value={BLANK_PRESET}>{BLANK_PRESET}</SelectItem>
                  {presets.map((p) => (
                    <SelectItem key={p.name} value={p.name}>
                      {p.name}
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
            <span className="text-sm text-muted-foreground">
              预设只是帮你填地址，Key 还得自己贴。
            </span>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>模型</CardTitle>
          <CardDescription>
            每个模型选一个供应商，采样参数各存各的。测试会先保存当前设置。
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {models.map((m, i) => (
            <div key={i} className="flex flex-col gap-4 rounded-lg border p-4">
              <div className="flex items-center gap-2">
                <Input
                  aria-label="模型显示名"
                  className="font-medium"
                  value={m.displayName}
                  placeholder="显示名，例如 看图的模型"
                  onChange={(e) =>
                    updateModel(i, { ...m, displayName: e.target.value })
                  }
                />
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label="测试连接"
                  disabled={testing !== "" || saving}
                  onClick={() => void test(m.id)}
                >
                  {testing === m.id ? <Spinner /> : <PlugZapIcon />}
                </Button>
                <Button
                  variant="ghost"
                  size="icon"
                  aria-label="删除模型"
                  onClick={() => removeModel(i)}
                >
                  <Trash2Icon />
                </Button>
              </div>
              <FieldGroup>
                <Field orientation="responsive">
                  <Field>
                    <FieldLabel htmlFor={`m-${i}-model`}>
                      模型名（发给接口的那个）
                    </FieldLabel>
                    <Input
                      id={`m-${i}-model`}
                      className="font-mono"
                      value={m.model}
                      placeholder="gpt-4o"
                      onChange={(e) =>
                        updateModel(i, { ...m, model: e.target.value })
                      }
                    />
                  </Field>
                  <Field>
                    <FieldLabel htmlFor={`m-${i}-provider`}>供应商</FieldLabel>
                    <Select
                      value={m.providerId}
                      onValueChange={(value) =>
                        updateModel(i, { ...m, providerId: value ?? "" })
                      }
                    >
                      <SelectTrigger id={`m-${i}-provider`} className="w-full">
                        <SelectValue placeholder="选一个供应商" />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectGroup>
                          {providers.map((p) => (
                            <SelectItem key={p.id} value={p.id}>
                              {p.name || p.id}
                            </SelectItem>
                          ))}
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                  </Field>
                </Field>
                <Field orientation="responsive">
                  <Field>
                    <FieldLabel htmlFor={`m-${i}-id`}>id</FieldLabel>
                    <Input
                      id={`m-${i}-id`}
                      className="font-mono"
                      value={m.id}
                      onChange={(e) =>
                        updateModel(i, { ...m, id: e.target.value })
                      }
                    />
                  </Field>
                  <Field>
                    <FieldLabel htmlFor={`m-${i}-max`}>
                      最大输出 token
                    </FieldLabel>
                    <Input
                      id={`m-${i}-max`}
                      type="number"
                      value={m.maxTokens}
                      onChange={(e) =>
                        updateModel(i, {
                          ...m,
                          maxTokens: Number(e.target.value),
                        })
                      }
                    />
                  </Field>
                  <Field>
                    <FieldLabel htmlFor={`m-${i}-temp`}>temperature</FieldLabel>
                    <Input
                      id={`m-${i}-temp`}
                      type="number"
                      step="0.1"
                      value={m.temperature}
                      onChange={(e) =>
                        updateModel(i, {
                          ...m,
                          temperature: Number(e.target.value),
                        })
                      }
                    />
                  </Field>
                </Field>
                <Field>
                  <FieldLabel htmlFor={`m-${i}-reasoning`}>思考强度</FieldLabel>
                  <Select
                    value={m.reasoningEffort || DEFAULT_REASONING}
                    onValueChange={(value) =>
                      updateModel(i, {
                        ...m,
                        reasoningEffort:
                          value === DEFAULT_REASONING ? "" : (value ?? ""),
                      })
                    }
                  >
                    <SelectTrigger
                      id={`m-${i}-reasoning`}
                      className="w-full sm:w-72"
                    >
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectGroup>
                        {REASONING_EFFORTS.map((effort) => (
                          <SelectItem key={effort.value} value={effort.value}>
                            {effort.label}
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                  <FieldDescription>
                    设置后会发送 reasoning_effort，改用
                    max_completion_tokens，并省略 temperature。
                  </FieldDescription>
                </Field>
              </FieldGroup>
            </div>
          ))}

          <div>
            <Button variant="outline" size="sm" onClick={addModel}>
              <PlusIcon data-icon="inline-start" />
              添加模型
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>分工</CardTitle>
          <CardDescription>
            视觉模型提取事实，快速 question model 只追问关键缺口，多模态 writer
            负责最终生成和后续修改。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <FieldGroup>
            <Field orientation="responsive">
              <Field>
                <FieldLabel htmlFor="role-vision">视觉模型</FieldLabel>
                <Select
                  value={cfg.vision.modelId}
                  onValueChange={(value) =>
                    patch("vision", { ...cfg.vision, modelId: value ?? "" })
                  }
                >
                  <SelectTrigger id="role-vision" className="w-full">
                    <SelectValue placeholder="选一个模型" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      {models.map((m) => (
                        <SelectItem key={m.id} value={m.id}>
                          {labelOf(m)}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FieldDescription>
                  必须能读图，参考图以 base64 内联发送。
                </FieldDescription>
              </Field>
              <Field>
                <FieldLabel htmlFor="v-edge">图片最长边</FieldLabel>
                <Input
                  id="v-edge"
                  type="number"
                  value={cfg.vision.imageMaxEdge}
                  onChange={(e) =>
                    patch("vision", {
                      ...cfg.vision,
                      imageMaxEdge: Number(e.target.value),
                    })
                  }
                />
                <FieldDescription>
                  上传前缩到这个尺寸，0 表示原图
                </FieldDescription>
              </Field>
            </Field>
            <Field>
              <FieldLabel htmlFor="role-question">Question model</FieldLabel>
              <Select
                value={cfg.question.modelId}
                onValueChange={(value) =>
                  patch("question", { modelId: value ?? "" })
                }
              >
                <SelectTrigger id="role-question" className="w-full">
                  <SelectValue placeholder="选一个模型" />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {models.map((m) => (
                      <SelectItem key={m.id} value={m.id}>
                        {labelOf(m)}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <FieldDescription>
                建议选择速度快的小模型。每轮最多 2 个问题、最多 3
                轮追问；输出长度使用所选模型的“最大输出
                token”，可在上方模型卡修改。
              </FieldDescription>
            </Field>
            <Field orientation="responsive">
              <Field>
                <FieldLabel htmlFor="role-writer">多模态 writer</FieldLabel>
                <Select
                  value={cfg.writer.modelId}
                  onValueChange={(value) =>
                    patch("writer", {
                      ...cfg.writer,
                      modelId: value ?? "",
                    })
                  }
                >
                  <SelectTrigger id="role-writer" className="w-full">
                    <SelectValue placeholder="选一个模型" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      {models.map((m) => (
                        <SelectItem key={m.id} value={m.id}>
                          {labelOf(m)}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FieldDescription>
                  必须能读图。生成、返修和用户继续修改时都会收到原始
                  skill、答案以及全部参考图。
                </FieldDescription>
              </Field>
              <Field>
                <FieldLabel htmlFor="w-edge">writer 图片最长边</FieldLabel>
                <Input
                  id="w-edge"
                  type="number"
                  value={cfg.writer.imageMaxEdge}
                  onChange={(e) =>
                    patch("writer", {
                      ...cfg.writer,
                      imageMaxEdge: Number(e.target.value),
                    })
                  }
                />
                <FieldDescription>0 表示把原图直接发给 writer</FieldDescription>
              </Field>
            </Field>
          </FieldGroup>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>校验</CardTitle>
        </CardHeader>
        <CardContent>
          <FieldGroup>
            <Field orientation="horizontal">
              <Switch
                id="strict"
                checked={cfg.strictEnglish}
                onCheckedChange={(v) => patch("strictEnglish", Boolean(v))}
              />
              <FieldLabel htmlFor="strict">
                正文出现中日韩文字时判为错误
              </FieldLabel>
            </Field>
            <FieldDescription>
              规范要求改写正文全英文，只有{" "}
              <span className="font-mono">&lt;d&gt;</span>{" "}
              内的台词和英文双引号里的画面文字保留原语言。关掉后只给提醒，不阻断。
            </FieldDescription>
            <Field>
              <FieldLabel htmlFor="repair">自动修复轮数上限</FieldLabel>
              <Input
                id="repair"
                type="number"
                value={cfg.maxRepairRounds}
                onChange={(e) =>
                  patch("maxRepairRounds", Number(e.target.value))
                }
              />
              <FieldDescription>
                校验不通过时，把错误清单回炉给模型重写的最大次数。
              </FieldDescription>
            </Field>
          </FieldGroup>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>访问</CardTitle>
          <CardDescription>
            监听地址：<span className="font-mono">{cfg.listen}</span>
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <Alert>
            <AlertTitle>局域网访问</AlertTitle>
            <AlertDescription>
              服务默认绑 0.0.0.0，局域网内任何人都能打开。要限制的话，在
              config.json 里设置 token 后重启，再在这里填同一个值。
            </AlertDescription>
          </Alert>
          <Field>
            <FieldLabel htmlFor="token">本浏览器使用的访问令牌</FieldLabel>
            <Input
              id="token"
              value={browserToken}
              onChange={(e) => setBrowserToken(e.target.value)}
              placeholder="没设置就留空"
            />
            <FieldDescription>
              只存在这台浏览器的 localStorage 里。
            </FieldDescription>
          </Field>
          <div>
            <Button
              variant="outline"
              size="sm"
              onClick={() => {
                setToken(browserToken.trim())
                toast.success("令牌已保存到本浏览器")
              }}
            >
              保存令牌
            </Button>
          </div>
        </CardContent>
      </Card>

      <div className="flex items-center gap-3">
        <Button onClick={() => void persist()} disabled={saving}>
          {saving ? (
            <Spinner data-icon="inline-start" />
          ) : (
            <SaveIcon data-icon="inline-start" />
          )}
          保存配置
        </Button>
        <Badge variant="outline">修改 ComfyUI 目录后会立刻重新扫描工作流</Badge>
      </div>
    </div>
  )
}
