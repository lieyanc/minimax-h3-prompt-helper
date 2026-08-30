import { ImageOffIcon, PencilIcon, UploadIcon } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { taskImageURL, type Task } from "@/lib/api"

/**
 * The rail that keeps every decision already fixed in view while the wizard
 * moves on: what the workflow dictates, which pictures are attached, and every
 * answer given so far — each one re-openable without losing the rest.
 */
export function FixedParams({
  task,
  onReask,
  onJump,
}: {
  task: Task
  onReask: (slot: string) => void
  onJump: (step: Task["step"]) => void
}) {
  const c = task.constraints
  const labels = [...c.pictureLabels, ...c.videoLabels, ...c.audioLabels]
  const answered = answeredList(task)

  return (
    <aside className="flex flex-col gap-4 rounded-xl border bg-card/40 p-4 text-sm lg:sticky lg:top-20 lg:self-start">
      <div className="flex flex-col gap-2">
        <Row label="已固定的参数" />
        <div className="flex flex-wrap items-center gap-1.5">
          <Badge>{c.mode}</Badge>
          <span className="truncate font-mono text-[11px] text-muted-foreground">
            {task.workflowName}
          </span>
        </div>
        <dl className="grid grid-cols-2 gap-2 text-xs">
          <Fact label="画布" value={`${c.width}×${c.height}`} />
          <Fact label="比例" value={c.aspectLabel || "—"} />
          <Fact label="时长" value={`${c.duration.toFixed(2)}s`} />
          <Fact label="帧数" value={String(c.frames)} />
          <Fact label="最多分镜" value={String(c.maxShots)} />
          <Fact label="参考资产" value={String(labels.length)} />
        </dl>
        {labels.length > 0 ? (
          <div className="flex flex-wrap gap-1">
            {labels.map((l) => (
              <Badge
                key={l}
                variant="outline"
                className="font-mono text-[10px]"
              >
                {l}
              </Badge>
            ))}
          </div>
        ) : null}
        <Button
          size="xs"
          variant="ghost"
          className="self-start"
          onClick={() => onJump("constraints")}
        >
          <PencilIcon data-icon="inline-start" />
          改时长
        </Button>
      </div>

      {task.images.length > 0 ? (
        <>
          <Separator />
          <div className="flex flex-col gap-2">
            <Row label="参考图" />
            <div className="flex flex-col gap-2">
              {task.images.map((img) => (
                <div key={img.label} className="flex items-center gap-2">
                  <div className="flex size-10 shrink-0 items-center justify-center overflow-hidden rounded-md border bg-muted">
                    {img.missing ? (
                      <ImageOffIcon className="size-4 text-muted-foreground" />
                    ) : (
                      <img
                        src={taskImageURL(task.id, img)}
                        alt={img.label}
                        loading="lazy"
                        className="size-full object-cover"
                      />
                    )}
                  </div>
                  <div className="flex min-w-0 flex-col">
                    <span className="font-mono text-[11px]">{img.label}</span>
                    <span className="truncate text-[11px] text-muted-foreground">
                      {img.origin === "upload" ? (
                        <span className="inline-flex items-center gap-1">
                          <UploadIcon className="size-3" />
                          {img.file}
                        </span>
                      ) : (
                        img.source || "（未接入）"
                      )}
                    </span>
                  </div>
                </div>
              ))}
            </div>
            <Button
              size="xs"
              variant="ghost"
              className="self-start"
              onClick={() => onJump("images")}
            >
              <PencilIcon data-icon="inline-start" />
              改参考图
            </Button>
          </div>
        </>
      ) : null}

      <Separator />
      <div className="flex flex-col gap-2">
        <Row label={`已确定 ${answered.length} 项`} />
        {answered.length === 0 ? (
          <p className="text-xs text-muted-foreground">还没有回答任何问题。</p>
        ) : (
          <ul className="flex flex-col gap-2">
            {answered.map((a) => (
              <li key={a.slot} className="flex flex-col gap-0.5">
                <div className="flex items-start gap-1">
                  <span className="flex-1 text-[11px] text-muted-foreground">
                    {a.title}
                  </span>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    aria-label="重新回答"
                    onClick={() => onReask(a.slot)}
                  >
                    <PencilIcon />
                  </Button>
                </div>
                <span className="line-clamp-3 text-xs whitespace-pre-wrap">
                  {a.value}
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>

      {task.comfyNotices.length > 0 ? (
        <>
          <Separator />
          <div className="flex flex-col gap-1">
            <Row label="ComfyUI 待办" />
            <p className="text-xs text-muted-foreground">
              有 {task.comfyNotices.length} 张参考图还要在 ComfyUI
              里上传或接线。
            </p>
          </div>
        </>
      ) : null}
    </aside>
  )
}

function Row({ label }: { label: string }) {
  return (
    <span className="text-xs font-medium tracking-wide text-muted-foreground uppercase">
      {label}
    </span>
  )
}

function Fact({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-[11px] text-muted-foreground">{label}</dt>
      <dd className="font-mono">{value}</dd>
    </div>
  )
}

type Answered = { slot: string; title: string; value: string }

/** Answers in the order they were asked, titled with the wording used. */
function answeredList(task: Task): Answered[] {
  const out: Answered[] = []
  const seen = new Set<string>()
  for (const q of task.asked ?? []) {
    const value = (task.answers?.[q.slot] ?? "").trim()
    if (!value || seen.has(q.slot)) continue
    seen.add(q.slot)
    out.push({ slot: q.slot, title: q.title, value })
  }
  for (const [slot, value] of Object.entries(task.answers ?? {})) {
    if (seen.has(slot) || !value.trim()) continue
    out.push({ slot, title: slot, value: value.trim() })
  }
  return out
}
