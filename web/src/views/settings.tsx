import { useEffect, useState } from "react"
import { PlugZapIcon, SaveIcon } from "lucide-react"
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
import { api, getToken, setToken, type Config } from "@/lib/api"

// Shown when the endpoint does not match any built-in preset.
const CUSTOM_PRESET = "自定义"

export function SettingsView() {
  const [cfg, setCfg] = useState<Config | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState("")
  const [visionKey, setVisionKey] = useState("")
  const [writerKey, setWriterKey] = useState("")
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

  const patch = <K extends keyof Config>(key: K, value: Config[K]) =>
    setCfg({ ...cfg, [key]: value })

  const save = async () => {
    setSaving(true)
    try {
      const body: Record<string, unknown> = {
        comfyuiRoot: cfg.comfyuiRoot,
        workflowDirs: dirs
          .split("\n")
          .map((s) => s.trim())
          .filter(Boolean),
        strictEnglish: cfg.strictEnglish,
        maxRepairRounds: cfg.maxRepairRounds,
        vision: {
          baseURL: cfg.vision.baseURL,
          model: cfg.vision.model,
          maxTokens: cfg.vision.maxTokens,
          temperature: cfg.vision.temperature,
          imageMaxEdge: cfg.vision.imageMaxEdge,
          ...(visionKey ? { apiKey: visionKey } : {}),
        },
        writer: {
          sameAsVision: cfg.writer.sameAsVision,
          baseURL: cfg.writer.baseURL,
          model: cfg.writer.model,
          maxTokens: cfg.writer.maxTokens,
          temperature: cfg.writer.temperature,
          ...(writerKey ? { apiKey: writerKey } : {}),
        },
      }
      const next = await api.saveConfig(body)
      setCfg(next)
      setVisionKey("")
      setWriterKey("")
      toast.success("配置已保存")
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setSaving(false)
    }
  }

  const test = async (target: "vision" | "writer") => {
    setTesting(target)
    try {
      const res = await api.testConfig(target)
      if (res.ok) toast.success(`连通，模型 ${res.model} 回了：${res.reply}`)
      else toast.error(res.error ?? "连接失败")
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setTesting("")
    }
  }

  const presets = cfg.presets ?? []
  const activePreset =
    presets.find((p) => p.baseURL === cfg.vision.baseURL)?.name ?? CUSTOM_PRESET
  const presetNote = presets.find((p) => p.name === activePreset)?.note

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
          <CardTitle>视觉模型</CardTitle>
          <CardDescription>
            任何 OpenAI 兼容的 /chat/completions 接口都能接，图片以 base64 内联发送。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="v-preset">常用接口</FieldLabel>
              <Select
                value={activePreset}
                onValueChange={(value) => {
                  const preset = presets.find((p) => p.name === value)
                  if (!preset) return
                  patch("vision", {
                    ...cfg.vision,
                    baseURL: preset.baseURL,
                    model: preset.model || cfg.vision.model,
                  })
                }}
              >
                <SelectTrigger id="v-preset" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    <SelectItem value={CUSTOM_PRESET}>
                      {CUSTOM_PRESET}
                    </SelectItem>
                    {presets.map((p) => (
                      <SelectItem key={p.name} value={p.name}>
                        {p.name}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
              <FieldDescription>
                {presetNote ??
                  "只是帮你填地址和一个常见模型名，具体模型请按供应商的文档改。"}
              </FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="v-base">接口地址</FieldLabel>
              <Input
                id="v-base"
                value={cfg.vision.baseURL}
                placeholder="https://api.example.com/v1"
                onChange={(e) =>
                  patch("vision", { ...cfg.vision, baseURL: e.target.value })
                }
              />
              <FieldDescription>
                不要带 /chat/completions，程序会自己拼。
              </FieldDescription>
            </Field>
            <Field>
              <FieldLabel htmlFor="v-model">模型名</FieldLabel>
              <Input
                id="v-model"
                value={cfg.vision.model}
                onChange={(e) =>
                  patch("vision", { ...cfg.vision, model: e.target.value })
                }
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="v-key">API Key</FieldLabel>
              <Input
                id="v-key"
                type="password"
                value={visionKey}
                placeholder={cfg.visionHasKey ? "已保存，留空则不修改" : "未设置"}
                onChange={(e) => setVisionKey(e.target.value)}
              />
            </Field>
            <Field orientation="responsive">
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
                <FieldDescription>上传前缩到这个尺寸，0 表示原图</FieldDescription>
              </Field>
              <Field>
                <FieldLabel htmlFor="v-temp">temperature</FieldLabel>
                <Input
                  id="v-temp"
                  type="number"
                  step="0.1"
                  value={cfg.vision.temperature}
                  onChange={(e) =>
                    patch("vision", {
                      ...cfg.vision,
                      temperature: Number(e.target.value),
                    })
                  }
                />
              </Field>
            </Field>
            <div>
              <Button
                variant="outline"
                size="sm"
                disabled={testing !== ""}
                onClick={() => void test("vision")}
              >
                {testing === "vision" ? (
                  <Spinner data-icon="inline-start" />
                ) : (
                  <PlugZapIcon data-icon="inline-start" />
                )}
                测试连接
              </Button>
            </div>
          </FieldGroup>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>写作模型</CardTitle>
          <CardDescription>
            负责把槽位和约束写成最终提示词。可以和视觉模型用同一个接口。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <FieldGroup>
            <Field orientation="horizontal">
              <Switch
                id="same"
                checked={cfg.writer.sameAsVision}
                onCheckedChange={(v) =>
                  patch("writer", { ...cfg.writer, sameAsVision: Boolean(v) })
                }
              />
              <FieldLabel htmlFor="same">与视觉模型使用同一个接口</FieldLabel>
            </Field>

            {!cfg.writer.sameAsVision ? (
              <>
                <Field>
                  <FieldLabel htmlFor="w-base">接口地址</FieldLabel>
                  <Input
                    id="w-base"
                    value={cfg.writer.baseURL}
                    onChange={(e) =>
                      patch("writer", { ...cfg.writer, baseURL: e.target.value })
                    }
                  />
                </Field>
                <Field>
                  <FieldLabel htmlFor="w-key">API Key</FieldLabel>
                  <Input
                    id="w-key"
                    type="password"
                    value={writerKey}
                    placeholder={
                      cfg.writerHasKey ? "已保存，留空则不修改" : "未设置"
                    }
                    onChange={(e) => setWriterKey(e.target.value)}
                  />
                </Field>
              </>
            ) : null}

            <Field>
              <FieldLabel htmlFor="w-model">模型名</FieldLabel>
              <Input
                id="w-model"
                value={cfg.writer.model}
                placeholder="留空则沿用视觉模型"
                onChange={(e) =>
                  patch("writer", { ...cfg.writer, model: e.target.value })
                }
              />
            </Field>
            <Field orientation="responsive">
              <Field>
                <FieldLabel htmlFor="w-max">max_tokens</FieldLabel>
                <Input
                  id="w-max"
                  type="number"
                  value={cfg.writer.maxTokens}
                  onChange={(e) =>
                    patch("writer", {
                      ...cfg.writer,
                      maxTokens: Number(e.target.value),
                    })
                  }
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="w-temp">temperature</FieldLabel>
                <Input
                  id="w-temp"
                  type="number"
                  step="0.1"
                  value={cfg.writer.temperature}
                  onChange={(e) =>
                    patch("writer", {
                      ...cfg.writer,
                      temperature: Number(e.target.value),
                    })
                  }
                />
              </Field>
            </Field>
            <div>
              <Button
                variant="outline"
                size="sm"
                disabled={testing !== ""}
                onClick={() => void test("writer")}
              >
                {testing === "writer" ? (
                  <Spinner data-icon="inline-start" />
                ) : (
                  <PlugZapIcon data-icon="inline-start" />
                )}
                测试连接
              </Button>
            </div>
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
              规范要求改写正文全英文，只有 <span className="font-mono">&lt;d&gt;</span>{" "}
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
        <Button onClick={() => void save()} disabled={saving}>
          {saving ? (
            <Spinner data-icon="inline-start" />
          ) : (
            <SaveIcon data-icon="inline-start" />
          )}
          保存配置
        </Button>
        <Badge variant="outline">
          修改 ComfyUI 目录后会立刻重新扫描工作流
        </Badge>
      </div>
    </div>
  )
}
