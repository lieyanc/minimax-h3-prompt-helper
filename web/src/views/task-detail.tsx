import { useCallback, useEffect, useRef, useState } from "react"
import {
  ArrowLeftIcon,
  CopyIcon,
  DownloadIcon,
  EyeIcon,
  ImageOffIcon,
  PencilIcon,
  PlayIcon,
  RefreshCwIcon,
  ScanEyeIcon,
} from "lucide-react"
import { toast } from "sonner"

import { navigate } from "@/App"
import { Findings } from "@/components/findings"
import { QuestionForm } from "@/components/question-form"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { Progress } from "@/components/ui/progress"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { Textarea } from "@/components/ui/textarea"
import {
  api,
  imageURL,
  streamPost,
  type Facts,
  type Finding,
  type Task,
} from "@/lib/api"

const GRID_FRAMES = Array.from({ length: 40 }, (_, i) => 5 + i * 17).filter(
  (f) => f / 24 >= 4 && f / 24 <= 15
)

export function TaskDetailView({ id }: { id: string }) {
  const [task, setTask] = useState<Task | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [analyzing, setAnalyzing] = useState(false)
  const [generating, setGenerating] = useState(false)
  const [streamText, setStreamText] = useState("")
  const [liveFindings, setLiveFindings] = useState<Finding[] | null>(null)
  const [repairRound, setRepairRound] = useState(0)
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState("")
  const streamRef = useRef<HTMLPreElement>(null)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      setTask(await api.task(id))
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [id])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    streamRef.current?.scrollTo({ top: streamRef.current.scrollHeight })
  }, [streamText])

  const submitAnswers = async (answers: Record<string, string>) => {
    setBusy(true)
    try {
      setTask(await api.patchTask(id, { answers }))
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const clearAnswer = async (slot: string) => {
    setBusy(true)
    try {
      setTask(await api.patchTask(id, { answers: { [slot]: "" } }))
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const setFrames = async (frames: number) => {
    setBusy(true)
    try {
      setTask(await api.patchTask(id, { frames }))
      toast.success("已更新时长约束")
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const analyze = async () => {
    setAnalyzing(true)
    try {
      await streamPost(`/api/tasks/${id}/analyze`, {}, (evt) => {
        if (evt.event === "image") {
          const payload = evt.data as { label: string; error?: string }
          if (payload.error) toast.error(`${payload.label}: ${payload.error}`)
        }
        if (evt.event === "done") setTask(evt.data as Task)
      })
      toast.success("参考图分析完成")
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setAnalyzing(false)
    }
  }

  const generate = async () => {
    setGenerating(true)
    setStreamText("")
    setLiveFindings(null)
    setRepairRound(0)
    try {
      await streamPost(`/api/tasks/${id}/generate`, {}, (evt) => {
        switch (evt.event) {
          case "delta":
            setStreamText((prev) => prev + (evt.data as { text: string }).text)
            break
          case "repair": {
            const d = evt.data as { round: number }
            setRepairRound(d.round)
            setStreamText("")
            toast.message(`第 ${d.round} 轮自动修复中`)
            break
          }
          case "validated":
            setLiveFindings((evt.data as { findings: Finding[] }).findings)
            break
          case "done":
            setTask(evt.data as Task)
            setStreamText("")
            setLiveFindings(null)
            toast.success("提示词已生成")
            break
          case "error":
            toast.error((evt.data as { error: string }).error)
            break
        }
      })
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setGenerating(false)
    }
  }

  const savePrompt = async () => {
    setBusy(true)
    try {
      setTask(await api.patchTask(id, { prompt: draft }))
      setEditing(false)
      toast.success("已保存并重新校验")
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  if (loading) return <Skeleton className="h-96 w-full rounded-xl" />
  if (!task)
    return (
      <Alert variant="destructive">
        <AlertTitle>任务不存在</AlertTitle>
        <AlertDescription>它可能已经被删除了。</AlertDescription>
      </Alert>
    )

  const c = task.constraints
  const analyzed = Object.keys(task.facts ?? {}).length
  const progress =
    task.requiredCount > 0
      ? Math.round((task.answeredCount / task.requiredCount) * 100)
      : 0

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center gap-3">
        <Button variant="ghost" size="sm" onClick={() => navigate("/tasks")}>
          <ArrowLeftIcon data-icon="inline-start" />
          任务列表
        </Button>
        <h1 className="text-lg font-semibold">{task.title}</h1>
        <Badge>{c.mode}</Badge>
        <span className="font-mono text-xs text-muted-foreground">
          {task.workflowName}
        </span>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>工作流约束</CardTitle>
          <CardDescription>
            这些值直接来自工作流，会注入写作提示并在生成后被校验器复查。
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <dl className="grid grid-cols-2 gap-4 text-sm sm:grid-cols-4">
            <div>
              <dt className="text-xs text-muted-foreground">画布</dt>
              <dd className="font-mono">
                {c.width}×{c.height} {c.aspectLabel}
              </dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">帧数</dt>
              <dd className="font-mono">{c.frames}</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">有效时长</dt>
              <dd className="font-mono">{c.duration.toFixed(2)}s</dd>
            </div>
            <div>
              <dt className="text-xs text-muted-foreground">最多分镜</dt>
              <dd className="font-mono">{c.maxShots}</dd>
            </div>
          </dl>

          <div className="flex flex-wrap gap-1.5">
            {[...c.pictureLabels, ...c.videoLabels, ...c.audioLabels].map(
              (l) => (
                <Badge key={l} variant="outline" className="font-mono">
                  {l}
                </Badge>
              )
            )}
            {c.pictureLabels.length === 0 &&
            c.videoLabels.length === 0 &&
            c.audioLabels.length === 0 ? (
              <span className="text-xs text-muted-foreground">
                没有接入参考资产，正文里不允许出现任何引用标签
              </span>
            ) : null}
          </div>

          {c.notes.length > 0 ? (
            <Alert>
              <AlertTitle>工作流提示</AlertTitle>
              <AlertDescription>
                <ul className="flex list-disc flex-col gap-1 pl-4">
                  {c.notes.map((n, i) => (
                    <li key={i}>{n}</li>
                  ))}
                </ul>
              </AlertDescription>
            </Alert>
          ) : null}

          <Separator />

          <div className="flex flex-col gap-2">
            <span className="text-xs text-muted-foreground">
              时长只能落在模型的 17k+5 帧网格上。改这里会同时改掉校验器的时间上限。
            </span>
            <div className="flex flex-wrap gap-1.5">
              {GRID_FRAMES.map((f) => (
                <Button
                  key={f}
                  size="xs"
                  variant={f === c.frames ? "secondary" : "outline"}
                  disabled={busy}
                  onClick={() => void setFrames(f)}
                >
                  {(f / 24).toFixed(2)}s
                </Button>
              ))}
            </div>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>参考图</CardTitle>
          <CardDescription>
            直接从 ComfyUI 的 input 目录读取，不需要重复上传。
          </CardDescription>
          <CardAction>
            <Button
              size="sm"
              variant="outline"
              onClick={() => void analyze()}
              disabled={analyzing || task.images.length === 0}
            >
              {analyzing ? (
                <Spinner data-icon="inline-start" />
              ) : (
                <ScanEyeIcon data-icon="inline-start" />
              )}
              {analyzed > 0 ? "重新分析" : "视觉分析"}
            </Button>
          </CardAction>
        </CardHeader>
        <CardContent>
          {task.images.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              这个分支没有接参考图（{c.mode} 模式）。
            </p>
          ) : (
            <div className="grid gap-4 md:grid-cols-2">
              {task.images.map((img) => (
                <ImageCard
                  key={img.label}
                  label={img.label}
                  slot={img.slot}
                  source={img.source}
                  missing={img.missing}
                  facts={task.facts?.[img.label]}
                />
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>互动追问</CardTitle>
          <CardDescription>
            只问规范要求、而视觉模型又推不出来的东西。每轮最多三个问题。
          </CardDescription>
          <CardAction>
            <Badge variant="outline">
              {task.answeredCount}/{task.requiredCount} 必答
            </Badge>
          </CardAction>
        </CardHeader>
        <CardContent className="flex flex-col gap-5">
          <Progress value={progress} />

          {task.questions.length > 0 ? (
            <QuestionForm
              questions={task.questions}
              busy={busy}
              onSubmit={(a) => void submitAnswers(a)}
            />
          ) : (
            <Alert>
              <AlertTitle>必答项都已完成</AlertTitle>
              <AlertDescription>
                可以生成提示词了。想改哪一项，在下面的清单里点重答。
              </AlertDescription>
            </Alert>
          )}

          {Object.keys(task.answers ?? {}).length > 0 ? (
            <>
              <Separator />
              <div className="flex flex-col gap-2">
                <span className="text-xs text-muted-foreground">已回答</span>
                <ul className="flex flex-col gap-1.5">
                  {Object.entries(task.answers)
                    .filter(([, v]) => v.trim() !== "")
                    .map(([slot, value]) => (
                      <li
                        key={slot}
                        className="flex items-start gap-2 text-sm"
                      >
                        <Badge
                          variant="outline"
                          className="mt-0.5 font-mono text-[10px]"
                        >
                          {slot}
                        </Badge>
                        <span className="flex-1 whitespace-pre-wrap">
                          {value}
                        </span>
                        <Button
                          variant="ghost"
                          size="icon-xs"
                          aria-label="重新回答"
                          disabled={busy}
                          onClick={() => void clearAnswer(slot)}
                        >
                          <PencilIcon />
                        </Button>
                      </li>
                    ))}
                </ul>
              </div>
            </>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>生成与校验</CardTitle>
          <CardDescription>
            生成后立刻跑一遍确定性校验，有阻断性错误会自动把问题回炉重写。
          </CardDescription>
          <CardAction>
            <div className="flex gap-2">
              {task.prompt ? (
                <Button
                  size="sm"
                  variant="outline"
                  disabled={busy}
                  onClick={async () => {
                    setBusy(true)
                    try {
                      setTask(await api.revalidate(id))
                    } finally {
                      setBusy(false)
                    }
                  }}
                >
                  <RefreshCwIcon data-icon="inline-start" />
                  重新校验
                </Button>
              ) : null}
              <Button
                size="sm"
                onClick={() => void generate()}
                disabled={generating || task.missing.length > 0}
              >
                {generating ? (
                  <Spinner data-icon="inline-start" />
                ) : (
                  <PlayIcon data-icon="inline-start" />
                )}
                {task.prompt ? "重新生成" : "生成提示词"}
              </Button>
            </div>
          </CardAction>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {task.missing.length > 0 ? (
            <Alert>
              <AlertTitle>还不能生成</AlertTitle>
              <AlertDescription>
                以下必答项还空着：{task.missing.join("、")}
              </AlertDescription>
            </Alert>
          ) : null}

          {generating ? (
            <div className="flex flex-col gap-2">
              <span className="text-xs text-muted-foreground">
                {repairRound > 0
                  ? `第 ${repairRound} 轮修复中…`
                  : "正在写…"}
              </span>
              <pre
                ref={streamRef}
                className="max-h-80 overflow-auto rounded-lg border bg-muted/40 p-3 font-mono text-xs whitespace-pre-wrap"
              >
                {streamText || " "}
              </pre>
              {liveFindings ? <Findings findings={liveFindings} /> : null}
            </div>
          ) : null}

          {task.prompt && !generating ? (
            <>
              <div className="flex flex-wrap gap-2">
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => {
                    void navigator.clipboard.writeText(task.prompt)
                    toast.success("已复制到剪贴板")
                  }}
                >
                  <CopyIcon data-icon="inline-start" />
                  复制
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => downloadText(`${task.title}.txt`, task.prompt)}
                >
                  <DownloadIcon data-icon="inline-start" />
                  下载 .txt
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => {
                    setDraft(task.prompt)
                    setEditing((v) => !v)
                  }}
                >
                  <PencilIcon data-icon="inline-start" />
                  {editing ? "取消编辑" : "手动编辑"}
                </Button>
                {task.attempts?.length > 1 ? (
                  <Badge variant="outline">
                    共 {task.attempts.length} 次尝试
                  </Badge>
                ) : null}
              </div>

              {editing ? (
                <div className="flex flex-col gap-2">
                  <Textarea
                    value={draft}
                    rows={18}
                    className="font-mono text-xs"
                    onChange={(e) => setDraft(e.target.value)}
                  />
                  <div>
                    <Button size="sm" disabled={busy} onClick={() => void savePrompt()}>
                      保存并重新校验
                    </Button>
                  </div>
                </div>
              ) : (
                <pre className="overflow-auto rounded-lg border bg-muted/40 p-3 font-mono text-xs whitespace-pre-wrap">
                  {task.prompt}
                </pre>
              )}

              <Findings findings={task.findings ?? []} />
            </>
          ) : null}

          {task.error ? (
            <Alert variant="destructive">
              <AlertTitle>上次生成出错</AlertTitle>
              <AlertDescription>{task.error}</AlertDescription>
            </Alert>
          ) : null}
        </CardContent>
      </Card>
    </div>
  )
}

function ImageCard({
  label,
  slot,
  source,
  missing,
  facts,
}: {
  label: string
  slot: string
  source: string
  missing: boolean
  facts?: Facts
}) {
  return (
    <div className="flex gap-3 rounded-lg border p-3">
      <div className="flex size-28 shrink-0 items-center justify-center overflow-hidden rounded-md border bg-muted">
        {missing ? (
          <ImageOffIcon className="text-muted-foreground" />
        ) : (
          <img
            src={imageURL(source)}
            alt={label}
            loading="lazy"
            className="size-full object-cover"
          />
        )}
      </div>
      <div className="flex min-w-0 flex-1 flex-col gap-1.5">
        <div className="flex flex-wrap items-center gap-1.5">
          <Badge className="font-mono">{label}</Badge>
          <Badge variant="outline" className="font-mono text-[10px]">
            {slot}
          </Badge>
        </div>
        <p className="truncate font-mono text-[11px] text-muted-foreground">
          {source || "（未接入）"}
        </p>

        {missing ? (
          <p className="text-xs text-destructive">
            这张图不在 ComfyUI input 目录里，视觉分析会跳过它。
          </p>
        ) : facts?.error ? (
          <p className="text-xs text-destructive">{facts.error}</p>
        ) : facts ? (
          <div className="flex flex-col gap-1 text-xs">
            {facts.style ? (
              <span>
                <span className="text-muted-foreground">风格 </span>
                {facts.style}
              </span>
            ) : null}
            {facts.summary ? <p>{facts.summary}</p> : null}
            {facts.subjects?.length ? (
              <ul className="flex flex-col gap-0.5 text-muted-foreground">
                {facts.subjects.map((s, i) => (
                  <li key={i}>
                    · {s.name}（{s.kind}）{s.appearance}
                  </li>
                ))}
              </ul>
            ) : null}
            {facts.visibleText?.length ? (
              <span className="text-muted-foreground">
                画面文字：{facts.visibleText.join(" / ")}
              </span>
            ) : null}
          </div>
        ) : (
          <p className="flex items-center gap-1 text-xs text-muted-foreground">
            <EyeIcon className="size-3" />
            还没有做视觉分析
          </p>
        )}
      </div>
    </div>
  )
}

function downloadText(filename: string, text: string) {
  const blob = new Blob([text], { type: "text/plain;charset=utf-8" })
  const url = URL.createObjectURL(blob)
  const a = document.createElement("a")
  a.href = url
  a.download = filename
  a.click()
  URL.revokeObjectURL(url)
}
