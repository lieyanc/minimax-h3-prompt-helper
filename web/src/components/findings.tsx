import { CircleCheckIcon, CircleXIcon, TriangleAlertIcon } from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import {
  Item,
  ItemContent,
  ItemDescription,
  ItemGroup,
  ItemMedia,
  ItemTitle,
} from "@/components/ui/item"
import type { Finding } from "@/lib/api"

/** Renders the deterministic validator output. */
export function Findings({ findings }: { findings: Finding[] }) {
  if (findings.length === 0) {
    return (
      <Alert>
        <CircleCheckIcon />
        <AlertTitle>格式校验通过</AlertTitle>
        <AlertDescription>
          字段顺序、时间戳、引用标签、台词逐字一致性和语言检查都没有发现问题。
        </AlertDescription>
      </Alert>
    )
  }

  const errors = findings.filter((f) => f.severity === "error")
  const warns = findings.filter((f) => f.severity === "warn")

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        {errors.length > 0 ? (
          <Badge variant="destructive">{errors.length} 项错误</Badge>
        ) : (
          <Badge variant="secondary">没有阻断性错误</Badge>
        )}
        {warns.length > 0 ? (
          <Badge variant="outline">{warns.length} 项提醒</Badge>
        ) : null}
      </div>

      <ItemGroup className="gap-2">
        {[...errors, ...warns].map((f, i) => (
          <Item key={`${f.rule}-${i}`} variant="outline">
            <ItemMedia>
              {f.severity === "error" ? (
                <CircleXIcon className="text-destructive" />
              ) : (
                <TriangleAlertIcon className="text-muted-foreground" />
              )}
            </ItemMedia>
            <ItemContent>
              <ItemTitle className="flex flex-wrap items-center gap-2">
                {f.message}
                {f.line ? (
                  <Badge variant="outline" className="font-mono">
                    第 {f.line} 行
                  </Badge>
                ) : null}
                <Badge variant="ghost" className="font-mono text-[10px]">
                  {f.rule}
                </Badge>
              </ItemTitle>
              {f.detail ? (
                <ItemDescription>{f.detail}</ItemDescription>
              ) : null}
            </ItemContent>
          </Item>
        ))}
      </ItemGroup>
    </div>
  )
}
