import { useCallback, useEffect, useRef, useState } from "react"
import {
  ArrowLeftIcon,
  ArrowRightIcon,
  CheckIcon,
  CopyIcon,
  DownloadIcon,
  PencilIcon,
  PlayIcon,
  RefreshCwIcon,
  ScanEyeIcon,
  SendIcon,
  SkipForwardIcon,
  SparklesIcon,
} from "lucide-react"
import { toast } from "sonner"

import { navigate } from "@/App"
import { ComfyNoticeBanner, ComfyNotices } from "@/components/comfy-notices"
import { Findings } from "@/components/findings"
import { FixedParams } from "@/components/fixed-params"
import { QuestionForm } from "@/components/question-form"
import { ReferenceImages } from "@/components/reference-images"
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
import { Progress } from "@/components/ui/progress"
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { Textarea } from "@/components/ui/textarea"
import { api, streamPost, type Finding, type Step, type Task } from "@/lib/api"

const GRID_FRAMES = Array.from({ length: 40 }, (_, i) => 5 + i * 17).filter(
  (f) => f / 24 >= 4 && f / 24 <= 15
)

const STEPS: { key: Step; label: string; hint: string }[] = [
  { key: "constraints", label: "工作流约束", hint: "模式、画布和时长" },
  { key: "images", label: "参考图", hint: "确认、替换或补充" },
  { key: "questions", label: "追问", hint: "按 skill 逐页生成" },
  { key: "generate", label: "生成与校验", hint: "写提示词并复查" },
]

/**
 * One task, walked one page at a time. Everything already decided stays in the
 * rail on the left; the page on the right only ever asks for the next thing.
 */
export function TaskDetailView({ id }: { id: string }) {
  const [task, setTask] = useState<Task | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [planning, setPlanning] = useState(false)
  const [planFailed, setPlanFailed] = useState(false)
  const [analyzing, setAnalyzing] = useState(false)
  const [generating, setGenerating] = useState(false)
  const [reasoningText, setReasoningText] = useState("")
  const [streamText, setStreamText] = useState("")
  const [liveFindings, setLiveFindings] = useState<Finding[] | null>(null)
  const [repairRound, setRepairRound] = useState(0)
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState("")
  const [revisionText, setRevisionText] = useState("")
  const reasoningRef = useRef<HTMLPreElement>(null)
  const streamRef = useRef<HTMLPreElement>(null)

  const planNext = useCallback(
    async (force = false) => {
      setPlanning(true)
      setPlanFailed(false)
      try {
        setTask(await api.plan(id, force))
      } catch (e) {
        setPlanFailed(true)
        toast.error((e as Error).message)
      } finally {
        setPlanning(false)
      }
    },
    [id]
  )

  // The interview is planned one page at a time: whenever the wizard lands on
  // the question step with nothing left to answer, the agent writes the next
  // page from the answers that just came in.
  const settle = useCallback(
    async (next: Task) => {
      setTask(next)
      if (
        next.step === "questions" &&
        next.questions.length === 0 &&
        !next.plan?.done
      ) {
        await planNext()
      }
    },
    [planNext]
  )

  const load = useCallback(async () => {
    setLoading(true)
    try {
      await settle(await api.task(id))
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [id, settle])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    reasoningRef.current?.scrollTo({
      top: reasoningRef.current.scrollHeight,
    })
    streamRef.current?.scrollTo({ top: streamRef.current.scrollHeight })
  }, [reasoningText, streamText])

  const patch = async (body: unknown) => {
    setBusy(true)
    try {
      await settle(await api.patchTask(id, body))
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const step = task?.step ?? "constraints"
  const planDone = task?.plan?.done ?? false

  const analyze = async (): Promise<Task | null> => {
    setAnalyzing(true)
    const completed: { task: Task | null } = { task: null }
    try {
      await streamPost(`/api/tasks/${id}/analyze`, {}, (evt) => {
        if (evt.event === "image") {
          const payload = evt.data as { label: string; error?: string }
          if (payload.error) toast.error(`${payload.label}: ${payload.error}`)
        }
        if (evt.event === "done") {
          completed.task = evt.data as Task
        }
      })
      const finalTask = completed.task
      if (finalTask) await settle(finalTask)
      if (finalTask && finalTask.visionPending.length === 0) {
        toast.success("参考图分析完成")
      } else if (finalTask) {
        toast.error("还有参考图未能完成视觉分析")
      }
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setAnalyzing(false)
    }
    return completed.task
  }

  const upload = async (file: File, label?: string) => {
    setBusy(true)
    try {
      setTask(await api.uploadImage(id, file, label))
      toast.success(label ? `${label} 已替换` : "参考图已添加")
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const removeImage = async (label: string) => {
    setBusy(true)
    try {
      setTask(await api.deleteImage(id, label))
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const runWriter = async (
    path: string,
    body: unknown,
    successMessage: string
  ) => {
    let succeeded = false
    setGenerating(true)
    setReasoningText("")
    setStreamText("")
    setLiveFindings(null)
    setRepairRound(0)
    try {
      await streamPost(path, body, (evt) => {
        switch (evt.event) {
          case "reasoning":
            setReasoningText(
              (prev) => prev + (evt.data as { text: string }).text
            )
            break
          case "delta":
            setStreamText((prev) => prev + (evt.data as { text: string }).text)
            break
          case "repair": {
            const d = evt.data as { round: number }
            setRepairRound(d.round)
            setReasoningText("")
            setStreamText("")
            toast.message(`第 ${d.round} 轮自动修复中`)
            break
          }
          case "validated":
            setLiveFindings((evt.data as { findings: Finding[] }).findings)
            break
          case "done":
            succeeded = true
            setTask(evt.data as Task)
            setReasoningText("")
            setStreamText("")
            setLiveFindings(null)
            toast.success(successMessage)
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
    return succeeded
  }

  const generate = () =>
    runWriter(`/api/tasks/${id}/generate`, {}, "提示词已生成")

  const revise = async () => {
    const message = revisionText.trim()
    if (!message) return
    if (
      await runWriter(
        `/api/tasks/${id}/revise`,
        { message },
        "writer 已完成修改"
      )
    ) {
      setRevisionText("")
    }
  }

  const savePrompt = async () => {
    await patch({ prompt: draft })
    setEditing(false)
    toast.success("已保存并重新校验")
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
  const visionPending = task.visionPending ?? []
  const visionReady = visionPending.length === 0
  const progress =
    task.requiredCount > 0
      ? Math.round((task.answeredCount / task.requiredCount) * 100)
      : 0
  const enterQuestions = async () => {
    if (visionReady) {
      await patch({ step: "questions" })
      return
    }
    const analyzedTask = await analyze()
    if (analyzedTask && analyzedTask.visionPending.length === 0) {
      await patch({ step: "questions" })
    }
  }
  const goto = (next: Step) => {
    if (next === "questions") {
      void enterQuestions()
      return
    }
    void patch({ step: next })
  }

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

      <Stepper current={step} onGo={goto} />

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_18rem] lg:items-start">
        <div className="flex min-w-0 flex-col gap-6">
          {step === "constraints" ? (
            <Card>
              <CardHeader>
                <CardTitle>工作流约束</CardTitle>
                <CardDescription>
                  这些值直接来自工作流，会注入写作提示并在生成后被校验器复查。确认无误再往下走。
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
                    时长只能落在模型的 17k+5
                    帧网格上。改这里会同时改掉校验器的时间上限。
                  </span>
                  <div className="flex flex-wrap gap-1.5">
                    {GRID_FRAMES.map((f) => (
                      <Button
                        key={f}
                        size="xs"
                        variant={f === c.frames ? "secondary" : "outline"}
                        disabled={busy}
                        onClick={() => void patch({ frames: f })}
                      >
                        {(f / 24).toFixed(2)}s
                      </Button>
                    ))}
                  </div>
                </div>
              </CardContent>
              <CardFooter>
                <Button onClick={() => goto("images")} disabled={busy}>
                  下一步：参考图
                  <ArrowRightIcon data-icon="inline-end" />
                </Button>
              </CardFooter>
            </Card>
          ) : null}

          {step === "images" ? (
            <Card>
              <CardHeader>
                <CardTitle>参考图</CardTitle>
                <CardDescription>
                  工作流里接的图直接从 ComfyUI 的 input
                  目录读取。你也可以在这里替换或者补一张，
                  提问会按图里实际的内容来问。
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
              <CardContent className="flex flex-col gap-4">
                <ComfyNoticeBanner notices={task.comfyNotices} />
                {task.images.length > 0 && !visionReady ? (
                  <Alert>
                    <AlertTitle>进入第三步前必须完成视觉分析</AlertTitle>
                    <AlertDescription>
                      提问会把图里实际的人物、场景和文字作为上下文。尚未就绪：
                      {visionPending.join("；")}
                    </AlertDescription>
                  </Alert>
                ) : null}
                <ReferenceImages
                  task={task}
                  busy={busy || analyzing}
                  onUpload={(file, label) => void upload(file, label)}
                  onDelete={(label) => void removeImage(label)}
                />
              </CardContent>
              <CardFooter className="gap-2">
                <Button
                  variant="outline"
                  onClick={() => goto("constraints")}
                  disabled={busy}
                >
                  <ArrowLeftIcon data-icon="inline-start" />
                  上一步
                </Button>
                <Button
                  onClick={() => void enterQuestions()}
                  disabled={busy || analyzing}
                >
                  {analyzing ? (
                    <Spinner data-icon="inline-start" />
                  ) : !visionReady ? (
                    <ScanEyeIcon data-icon="inline-start" />
                  ) : null}
                  {visionReady ? "下一步：开始追问" : "分析并开始追问"}
                  <ArrowRightIcon data-icon="inline-end" />
                </Button>
              </CardFooter>
            </Card>
          ) : null}

          {step === "questions" ? (
            <Card>
              <CardHeader>
                <CardTitle className="flex flex-wrap items-center gap-2">
                  {task.plan?.title || "追问"}
                  {task.plan?.index ? (
                    <Badge variant="outline">第 {task.plan.index} 页</Badge>
                  ) : null}
                  {task.plan?.fallback ? (
                    <Badge variant="secondary">固定问题表</Badge>
                  ) : null}
                </CardTitle>
                <CardDescription>
                  {task.plan?.intro ||
                    "问题由模型按 h3-prompt-writing 规范和这组参考图现场生成，只问它还不知道的东西。"}
                </CardDescription>
                <CardAction>
                  <Badge variant="outline">已答 {task.answeredCount}</Badge>
                </CardAction>
              </CardHeader>
              <CardContent className="flex flex-col gap-5">
                <Progress value={progress} />

                {task.planError ? (
                  <Alert>
                    <AlertTitle>提问模型这一轮没用上</AlertTitle>
                    <AlertDescription>{task.planError}</AlertDescription>
                  </Alert>
                ) : null}

                {planning ? (
                  <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    <Spinner />
                    正在按规范决定下一页要问什么…
                  </div>
                ) : task.questions.length > 0 ? (
                  <QuestionForm
                    questions={task.questions}
                    busy={busy}
                    submitLabel="提交这一页"
                    onSubmit={(answers) =>
                      void patch({ answers, submitPage: true })
                    }
                  />
                ) : planFailed ? (
                  <div className="flex flex-col items-start gap-2">
                    <p className="text-sm text-muted-foreground">
                      没能拿到下一页问题。
                    </p>
                    <Button size="sm" onClick={() => void planNext(true)}>
                      <RefreshCwIcon data-icon="inline-start" />
                      重试
                    </Button>
                  </div>
                ) : (
                  <Alert>
                    <CheckIcon />
                    <AlertTitle>问完了</AlertTitle>
                    <AlertDescription>
                      {task.plan?.note ||
                        "规范要求的信息都齐了，可以生成提示词。"}
                    </AlertDescription>
                  </Alert>
                )}
              </CardContent>
              <CardFooter className="flex-wrap gap-2">
                <Button
                  variant="outline"
                  onClick={() => goto("images")}
                  disabled={busy}
                >
                  <ArrowLeftIcon data-icon="inline-start" />
                  上一步
                </Button>
                {!planDone && task.questions.length > 0 ? (
                  <Button
                    variant="ghost"
                    disabled={busy}
                    onClick={() =>
                      void patch({ skipQuestions: true, step: "generate" })
                    }
                  >
                    <SkipForwardIcon data-icon="inline-start" />
                    跳过剩余追问
                  </Button>
                ) : null}
                <Button
                  onClick={() => goto("generate")}
                  disabled={busy || task.missing.length > 0}
                >
                  下一步：生成
                  <ArrowRightIcon data-icon="inline-end" />
                </Button>
              </CardFooter>
            </Card>
          ) : null}

          {step === "generate" ? (
            <>
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

                  {!planDone && task.missing.length === 0 && !task.prompt ? (
                    <Alert>
                      <SparklesIcon />
                      <AlertTitle>追问还没走完</AlertTitle>
                      <AlertDescription>
                        现在也能生成，但回去多答几页，提示词会更贴合你的意图。
                      </AlertDescription>
                    </Alert>
                  ) : null}

                  {generating ? (
                    <div className="flex flex-col gap-3">
                      <span
                        className="flex items-center gap-2 text-xs text-muted-foreground"
                        aria-live="polite"
                      >
                        <Spinner className="size-3" />
                        {repairRound > 0
                          ? `第 ${repairRound} 轮修复中…`
                          : reasoningText && !streamText
                            ? "模型正在思考…"
                            : "正在实时生成…"}
                      </span>
                      {reasoningText ? (
                        <div className="flex flex-col gap-1">
                          <span className="text-xs font-medium text-muted-foreground">
                            思考过程（实时）
                          </span>
                          <pre
                            ref={reasoningRef}
                            className="max-h-48 overflow-auto rounded-lg border border-dashed bg-muted/20 p-3 font-mono text-xs whitespace-pre-wrap text-muted-foreground"
                          >
                            {reasoningText}
                          </pre>
                        </div>
                      ) : null}
                      <div className="flex flex-col gap-1">
                        <span className="text-xs font-medium text-muted-foreground">
                          提示词正文（实时）
                        </span>
                        <pre
                          ref={streamRef}
                          className="max-h-80 overflow-auto rounded-lg border bg-muted/40 p-3 font-mono text-xs whitespace-pre-wrap"
                        >
                          {streamText || "等待正文输出…"}
                        </pre>
                      </div>
                      {liveFindings ? (
                        <Findings findings={liveFindings} />
                      ) : null}
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
                          onClick={() =>
                            downloadText(`${task.title}.txt`, task.prompt)
                          }
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
                            共 {task.attempts.length} 次 writer 输出
                          </Badge>
                        ) : null}
                        {task.revisions?.length > 0 ? (
                          <Badge variant="secondary">
                            已修改 {task.revisions.length} 轮
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
                            <Button
                              size="sm"
                              disabled={busy}
                              onClick={() => void savePrompt()}
                            >
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

                      <Separator />

                      <form
                        onSubmit={(event) => {
                          event.preventDefault()
                          if (!generating) void revise()
                        }}
                      >
                        <FieldGroup>
                          <Field>
                            <FieldLabel htmlFor="writer-revision">
                              继续让 writer 修改
                            </FieldLabel>
                            <Textarea
                              id="writer-revision"
                              value={revisionText}
                              rows={3}
                              disabled={generating}
                              placeholder="例：保留人物动作，把镜头改成从全景缓慢推进到手部特写；不要改台词"
                              onChange={(event) =>
                                setRevisionText(event.target.value)
                              }
                            />
                            <FieldDescription>
                              每次修改都会重新注入原始
                              skill、工作流约束、全部答案和原始参考图，再执行完整校验。
                            </FieldDescription>
                          </Field>
                          <Field orientation="horizontal">
                            <Button
                              type="submit"
                              size="sm"
                              disabled={
                                generating || revisionText.trim().length === 0
                              }
                            >
                              {generating ? (
                                <Spinner data-icon="inline-start" />
                              ) : (
                                <SendIcon data-icon="inline-start" />
                              )}
                              发送修改要求
                            </Button>
                            {task.revisions?.length > 0 ? (
                              <FieldDescription>
                                writer 记得前 {task.revisions.length} 轮修改
                              </FieldDescription>
                            ) : null}
                          </Field>
                        </FieldGroup>
                      </form>
                    </>
                  ) : null}

                  {task.error ? (
                    <Alert variant="destructive">
                      <AlertTitle>上次生成出错</AlertTitle>
                      <AlertDescription>{task.error}</AlertDescription>
                    </Alert>
                  ) : null}
                </CardContent>
                <CardFooter>
                  <Button
                    variant="outline"
                    onClick={() => goto("questions")}
                    disabled={busy}
                  >
                    <ArrowLeftIcon data-icon="inline-start" />
                    回到追问
                  </Button>
                </CardFooter>
              </Card>

              <ComfyNotices notices={task.comfyNotices} />
            </>
          ) : null}
        </div>

        <FixedParams
          task={task}
          onReask={(slot) => void patch({ reask: slot, step: "questions" })}
          onJump={goto}
        />
      </div>
    </div>
  )
}

function Stepper({
  current,
  onGo,
}: {
  current: Step
  onGo: (step: Step) => void
}) {
  const index = STEPS.findIndex((s) => s.key === current)
  return (
    <ol className="flex flex-wrap gap-2">
      {STEPS.map((s, i) => {
        const state = i === index ? "current" : i < index ? "done" : "todo"
        return (
          <li key={s.key} className="flex-1 basis-40">
            <button
              type="button"
              onClick={() => onGo(s.key)}
              className={`flex w-full flex-col gap-0.5 rounded-lg border p-3 text-left transition-colors ${
                state === "current"
                  ? "border-primary bg-primary/5"
                  : "hover:bg-muted/50"
              }`}
            >
              <span className="flex items-center gap-2 text-sm font-medium">
                <span
                  className={`flex size-5 items-center justify-center rounded-full text-[10px] ${
                    state === "todo"
                      ? "bg-muted text-muted-foreground"
                      : "bg-primary text-primary-foreground"
                  }`}
                >
                  {state === "done" ? <CheckIcon className="size-3" /> : i + 1}
                </span>
                {s.label}
              </span>
              <span className="text-xs text-muted-foreground">{s.hint}</span>
            </button>
          </li>
        )
      })}
    </ol>
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
