import { useEffect, useMemo, useState } from "react"
import {
  AlertTriangleIcon,
  FolderSearchIcon,
  ImageOffIcon,
  RefreshCwIcon,
  SparklesIcon,
} from "lucide-react"
import { toast } from "sonner"

import { navigate } from "@/App"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Input } from "@/components/ui/input"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { api, imageURL, type Variant, type Workflow } from "@/lib/api"

const MODE_HINT: Record<string, string> = {
  T2VA: "纯文本构建完整时间线",
  I2VA: "从首帧向后发展",
  FL2VA: "首帧到末帧的连续路径",
  L2VA: "推演前置状态并收敛到末帧",
  Ref2VA: "多参考资产，六段式改写",
}

export function WorkflowsView() {
  const [loading, setLoading] = useState(true)
  const [workflows, setWorkflows] = useState<Workflow[]>([])
  const [dirs, setDirs] = useState<string[]>([])
  const [error, setError] = useState("")
  const [filter, setFilter] = useState("")
  const [creating, setCreating] = useState("")

  const load = async () => {
    setLoading(true)
    setError("")
    try {
      const res = await api.workflows()
      setWorkflows(res.workflows)
      setDirs(res.dirs)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
  }, [])

  const withVariants = useMemo(
    () =>
      workflows
        .filter((w) => w.variants.length > 0)
        .filter((w) =>
          filter
            ? w.name.toLowerCase().includes(filter.toLowerCase()) ||
              w.variants.some((v) =>
                v.mode.toLowerCase().includes(filter.toLowerCase())
              )
            : true
        ),
    [workflows, filter]
  )

  const others = useMemo(
    () => workflows.filter((w) => w.variants.length === 0),
    [workflows]
  )

  const start = async (wf: Workflow, v: Variant) => {
    setCreating(v.id + wf.file)
    try {
      const task = await api.createTask(wf.file, v.id)
      navigate(`/tasks/${task.id}`)
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setCreating("")
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h1 className="text-lg font-semibold">ComfyUI 工作流</h1>
          <p className="text-sm text-muted-foreground">
            扫描本机工作流，读出每个 H3 分支的模式、画布、帧数和实际接入的参考输入。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Input
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="按名称或模式过滤"
            className="w-56"
          />
          <Button variant="outline" size="sm" onClick={() => void load()}>
            <RefreshCwIcon data-icon="inline-start" />
            重新扫描
          </Button>
        </div>
      </div>

      {dirs.length > 0 ? (
        <p className="font-mono text-xs text-muted-foreground">
          扫描目录：{dirs.join("  ·  ")}
        </p>
      ) : null}

      {error ? (
        <Alert variant="destructive">
          <AlertTriangleIcon />
          <AlertTitle>扫描失败</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      {loading ? (
        <div className="grid gap-4 md:grid-cols-2">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-56 w-full rounded-xl" />
          ))}
        </div>
      ) : withVariants.length === 0 ? (
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <FolderSearchIcon />
            </EmptyMedia>
            <EmptyTitle>没有找到含 MiniMax H3 节点的工作流</EmptyTitle>
            <EmptyDescription>
              到设置页确认 ComfyUI 根目录，或者把工作流放进
              user/default/workflows。
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className="flex flex-col gap-8">
          {withVariants.map((wf) => (
            <section key={wf.file} className="flex flex-col gap-3">
              <div className="flex flex-wrap items-baseline gap-2">
                <h2 className="text-base font-medium">{wf.name}</h2>
                <Badge variant="secondary">{wf.variants.length} 个 H3 分支</Badge>
                <span className="font-mono text-xs text-muted-foreground">
                  {wf.file}
                </span>
              </div>

              {wf.models.unet.length > 0 || wf.models.lora.length > 0 ? (
                <p className="text-xs text-muted-foreground">
                  模型：{[...wf.models.unet, ...wf.models.lora].join(" · ")}
                </p>
              ) : null}

              <div className="grid gap-4 lg:grid-cols-2">
                {wf.variants.map((v) => (
                  <VariantCard
                    key={v.id}
                    workflow={wf}
                    variant={v}
                    busy={creating === v.id + wf.file}
                    onStart={() => void start(wf, v)}
                  />
                ))}
              </div>
            </section>
          ))}

          {others.length > 0 ? (
            <section className="flex flex-col gap-2">
              <Separator />
              <p className="text-sm text-muted-foreground">
                另有 {others.length} 个工作流没有 H3 节点：
                {others
                  .slice(0, 8)
                  .map((w) => w.name)
                  .join("、")}
                {others.length > 8 ? " 等" : ""}
              </p>
            </section>
          ) : null}
        </div>
      )}
    </div>
  )
}

function VariantCard({
  workflow,
  variant,
  busy,
  onStart,
}: {
  workflow: Workflow
  variant: Variant
  busy: boolean
  onStart: () => void
}) {
  const images = variant.images.filter((s) => s.connected)
  const videos = variant.videos.filter((s) => s.connected)
  const audios = variant.audios.filter((s) => s.connected)

  return (
    <Card className={variant.active ? "" : "opacity-70"}>
      <CardHeader>
        <CardTitle className="flex flex-wrap items-center gap-2">
          <Badge>{variant.mode}</Badge>
          {variant.active ? (
            <Badge variant="secondary">当前启用</Badge>
          ) : (
            <Badge variant="outline">已 bypass</Badge>
          )}
          <span className="font-mono text-xs font-normal text-muted-foreground">
            #{variant.nodeId} {variant.nodeType}
          </span>
        </CardTitle>
        <CardDescription>{MODE_HINT[variant.mode] ?? ""}</CardDescription>
        <CardAction>
          <Button size="sm" onClick={onStart} disabled={busy}>
            {busy ? <Spinner data-icon="inline-start" /> : <SparklesIcon data-icon="inline-start" />}
            用这个开始
          </Button>
        </CardAction>
      </CardHeader>

      <CardContent className="flex flex-col gap-3">
        <dl className="grid grid-cols-3 gap-3 text-sm">
          <div>
            <dt className="text-xs text-muted-foreground">画布</dt>
            <dd className="font-mono">
              {variant.width}×{variant.height}
            </dd>
          </div>
          <div>
            <dt className="text-xs text-muted-foreground">帧数 @ 24fps</dt>
            <dd className="font-mono">{variant.frames}</dd>
          </div>
          <div>
            <dt className="text-xs text-muted-foreground">有效时长</dt>
            <dd className="font-mono">{variant.duration.toFixed(2)}s</dd>
          </div>
        </dl>

        <div className="flex flex-wrap gap-1.5">
          <Badge variant="outline">{images.length} 张参考图</Badge>
          {videos.length > 0 ? (
            <Badge variant="outline">{videos.length} 段参考视频</Badge>
          ) : null}
          {audios.length > 0 ? (
            <Badge variant="outline">{audios.length} 条参考音频</Badge>
          ) : null}
          {variant.guides > 0 ? (
            <Badge variant="outline">{variant.guides} 个 AddGuide 锚点</Badge>
          ) : null}
          {!variant.derived ? (
            <Badge variant="destructive">参数可能过期</Badge>
          ) : null}
        </div>

        {images.length > 0 ? (
          <div className="flex flex-wrap gap-2">
            {images.map((slot) => (
              <figure key={slot.slot} className="flex w-24 flex-col gap-1">
                <div className="flex aspect-square items-center justify-center overflow-hidden rounded-md border bg-muted">
                  {slot.sourceExists ? (
                    <img
                      src={imageURL(slot.source)}
                      alt={slot.label}
                      loading="lazy"
                      className="size-full object-cover"
                    />
                  ) : (
                    <ImageOffIcon className="text-muted-foreground" />
                  )}
                </div>
                <figcaption className="truncate text-center font-mono text-[10px] text-muted-foreground">
                  {slot.label} · {slot.slot}
                </figcaption>
              </figure>
            ))}
          </div>
        ) : null}

        {variant.notes.length > 0 ? (
          <Alert>
            <AlertTriangleIcon />
            <AlertDescription>
              <ul className="flex flex-col gap-1">
                {variant.notes.map((n, i) => (
                  <li key={i}>{n}</li>
                ))}
              </ul>
            </AlertDescription>
          </Alert>
        ) : null}
      </CardContent>

      {variant.promptText ? (
        <CardFooter className="flex flex-col items-start gap-1">
          <span className="text-xs text-muted-foreground">
            工作流里现有的提示词（前 160 字）
          </span>
          <p className="line-clamp-3 font-mono text-xs text-muted-foreground">
            {variant.promptText.slice(0, 160)}
          </p>
        </CardFooter>
      ) : null}
      <span className="sr-only">{workflow.name}</span>
    </Card>
  )
}
