import { AlertTriangleIcon, UploadCloudIcon } from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import type { ComfyNotice } from "@/lib/api"

const KIND_LABEL: Record<ComfyNotice["kind"], string> = {
  replace: "要在 ComfyUI 里换图",
  add: "工作流里还没有这个输入",
  missing: "input 目录里找不到",
}

/**
 * What is still missing on the ComfyUI side. This tool only writes text: an
 * image attached here is not an image ComfyUI will load, so anything that does
 * not match the workflow has to be uploaded and wired there by hand.
 */
export function ComfyNotices({ notices }: { notices: ComfyNotice[] }) {
  if (notices.length === 0) return null
  return (
    <Alert>
      <UploadCloudIcon />
      <AlertTitle>还要回 ComfyUI 做这些事</AlertTitle>
      <AlertDescription>
        <ul className="flex flex-col gap-2">
          {notices.map((n) => (
            <li key={n.label + n.file} className="flex flex-col gap-1">
              <span className="flex flex-wrap items-center gap-1.5">
                <Badge variant="outline" className="font-mono text-[10px]">
                  {n.label}
                </Badge>
                <Badge
                  variant={n.kind === "missing" ? "destructive" : "secondary"}
                  className="text-[10px]"
                >
                  {KIND_LABEL[n.kind]}
                </Badge>
                {n.slot ? (
                  <span className="font-mono text-[10px] text-muted-foreground">
                    {n.slot}
                  </span>
                ) : null}
              </span>
              <span>{n.text}</span>
            </li>
          ))}
        </ul>
      </AlertDescription>
    </Alert>
  )
}

/** The same list, condensed to a single warning line. */
export function ComfyNoticeBanner({ notices }: { notices: ComfyNotice[] }) {
  if (notices.length === 0) return null
  return (
    <Alert>
      <AlertTriangleIcon />
      <AlertTitle>{notices.length} 张参考图和工作流对不上</AlertTitle>
      <AlertDescription>
        提示词照样能写，但生成前要在 ComfyUI
        里把这些图上传接好。完整清单在最后一步。
      </AlertDescription>
    </Alert>
  )
}
