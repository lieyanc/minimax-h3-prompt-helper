import { useRef, useState } from "react"
import {
  EyeIcon,
  ImageOffIcon,
  ImagePlusIcon,
  RefreshCwIcon,
  Trash2Icon,
  UploadIcon,
} from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Spinner } from "@/components/ui/spinner"
import { taskImageURL, type Facts, type Task, type TaskImage } from "@/lib/api"

/**
 * The reference images of a task: the ones the workflow loads, plus any the
 * user attaches by hand here. Uploads are kept with the task data and never
 * written into ComfyUI's own folders, so an image added here is a stand-in
 * until it is uploaded in ComfyUI too — which is what the notices at the end
 * are for.
 */
export function ReferenceImages({
  task,
  busy,
  onUpload,
  onDelete,
}: {
  task: Task
  busy: boolean
  onUpload: (file: File, label?: string) => void
  onDelete: (label: string) => void
}) {
  const [dragging, setDragging] = useState(false)
  const addInput = useRef<HTMLInputElement>(null)

  const pick = (files: FileList | null, label?: string) => {
    const file = files?.[0]
    if (file) onUpload(file, label)
  }

  return (
    <div className="flex flex-col gap-4">
      {task.images.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          这个分支没有接参考图（{task.constraints.mode}{" "}
          模式）。你也可以在下面手动加一张。
        </p>
      ) : (
        <div className="grid gap-4 md:grid-cols-2">
          {task.images.map((img) => (
            <ImageCard
              key={img.label}
              taskId={task.id}
              image={img}
              facts={task.facts?.[img.label]}
              busy={busy}
              onReplace={(file) => onUpload(file, img.label)}
              onDelete={() => onDelete(img.label)}
            />
          ))}
        </div>
      )}

      <div
        onDragOver={(e) => {
          e.preventDefault()
          setDragging(true)
        }}
        onDragLeave={() => setDragging(false)}
        onDrop={(e) => {
          e.preventDefault()
          setDragging(false)
          pick(e.dataTransfer.files)
        }}
        className={`flex flex-col items-center gap-2 rounded-xl border border-dashed p-6 text-center transition-colors ${
          dragging ? "border-primary bg-primary/5" : "border-border"
        }`}
      >
        <ImagePlusIcon className="size-5 text-muted-foreground" />
        <p className="text-sm">把图片拖进来，或者点下面的按钮</p>
        <p className="text-xs text-muted-foreground">
          手动加的图会存在任务目录里，不会写进
          ComfyUI。工作流里没有的输入，最后会提醒你去 ComfyUI 上传接线。
        </p>
        <input
          ref={addInput}
          type="file"
          accept="image/*"
          className="hidden"
          onChange={(e) => {
            pick(e.target.files)
            e.target.value = ""
          }}
        />
        <Button
          size="sm"
          variant="outline"
          disabled={busy}
          onClick={() => addInput.current?.click()}
        >
          {busy ? (
            <Spinner data-icon="inline-start" />
          ) : (
            <UploadIcon data-icon="inline-start" />
          )}
          加一张参考图
        </Button>
      </div>
    </div>
  )
}

function ImageCard({
  taskId,
  image,
  facts,
  busy,
  onReplace,
  onDelete,
}: {
  taskId: string
  image: TaskImage
  facts?: Facts
  busy: boolean
  onReplace: (file: File) => void
  onDelete: () => void
}) {
  const input = useRef<HTMLInputElement>(null)

  return (
    <div className="flex gap-3 rounded-lg border p-3">
      <div className="flex size-28 shrink-0 items-center justify-center overflow-hidden rounded-md border bg-muted">
        {image.missing ? (
          <ImageOffIcon className="text-muted-foreground" />
        ) : (
          <img
            src={taskImageURL(taskId, image)}
            alt={image.label}
            loading="lazy"
            className="size-full object-cover"
          />
        )}
      </div>
      <div className="flex min-w-0 flex-1 flex-col gap-1.5">
        <div className="flex flex-wrap items-center gap-1.5">
          <Badge className="font-mono">{image.label}</Badge>
          <Badge variant="outline" className="font-mono text-[10px]">
            {image.slot}
          </Badge>
          {image.origin === "upload" ? (
            <Badge variant="secondary" className="text-[10px]">
              {image.wired ? "手动替换" : "工作流里没有"}
            </Badge>
          ) : null}
        </div>
        <p className="truncate font-mono text-[11px] text-muted-foreground">
          {image.origin === "upload"
            ? image.file
            : image.source || "（未接入）"}
        </p>

        {image.missing ? (
          <p className="text-xs text-destructive">
            这张图不在 ComfyUI input 目录里，视觉分析会跳过它。传一张替代它。
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

        <div className="mt-auto flex flex-wrap gap-1 pt-1">
          <input
            ref={input}
            type="file"
            accept="image/*"
            className="hidden"
            onChange={(e) => {
              const file = e.target.files?.[0]
              if (file) onReplace(file)
              e.target.value = ""
            }}
          />
          <Button
            size="xs"
            variant="outline"
            disabled={busy}
            onClick={() => input.current?.click()}
          >
            <RefreshCwIcon data-icon="inline-start" />
            换一张
          </Button>
          {image.origin === "upload" ? (
            <Button
              size="xs"
              variant="ghost"
              disabled={busy}
              onClick={onDelete}
            >
              <Trash2Icon data-icon="inline-start" />
              {image.wired ? "还原成工作流的图" : "移除"}
            </Button>
          ) : null}
        </div>
      </div>
    </div>
  )
}
