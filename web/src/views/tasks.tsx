import { useEffect, useState } from "react"
import { InboxIcon, RefreshCwIcon, Trash2Icon } from "lucide-react"
import { toast } from "sonner"

import { navigate } from "@/App"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { api, type TaskSummary } from "@/lib/api"

const STATUS_LABEL: Record<string, string> = {
  draft: "草稿",
  questioning: "追问中",
  ready: "待生成",
  done: "已生成",
  error: "出错",
}

export function TasksView() {
  const [tasks, setTasks] = useState<TaskSummary[]>([])
  const [loading, setLoading] = useState(true)

  const load = async () => {
    setLoading(true)
    try {
      const res = await api.tasks()
      setTasks(res.tasks)
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
  }, [])

  const remove = async (id: string) => {
    try {
      await api.deleteTask(id)
      setTasks((prev) => prev.filter((t) => t.id !== id))
      toast.success("任务已删除")
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h1 className="text-lg font-semibold">任务</h1>
          <p className="text-sm text-muted-foreground">
            每个任务是一次完整的提示词编写会话，保存在数据目录里的独立 JSON 文件中。
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={() => void load()}>
          <RefreshCwIcon data-icon="inline-start" />
          刷新
        </Button>
      </div>

      {loading ? (
        <Skeleton className="h-64 w-full rounded-xl" />
      ) : tasks.length === 0 ? (
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <InboxIcon />
            </EmptyMedia>
            <EmptyTitle>还没有任务</EmptyTitle>
            <EmptyDescription>
              到工作流页选一个 H3 分支开始。
            </EmptyDescription>
          </EmptyHeader>
        </Empty>
      ) : (
        <div className="overflow-hidden rounded-xl border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>标题</TableHead>
                <TableHead>模式</TableHead>
                <TableHead>时长</TableHead>
                <TableHead>参考图</TableHead>
                <TableHead>状态</TableHead>
                <TableHead>校验</TableHead>
                <TableHead>更新时间</TableHead>
                <TableHead className="w-10" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {tasks.map((t) => (
                <TableRow
                  key={t.id}
                  className="cursor-pointer"
                  onClick={() => navigate(`/tasks/${t.id}`)}
                >
                  <TableCell className="font-medium">
                    <div className="flex flex-col">
                      <span>{t.title}</span>
                      <span className="text-xs text-muted-foreground">
                        {t.workflowName}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline">{t.mode}</Badge>
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {t.duration.toFixed(2)}s
                  </TableCell>
                  <TableCell className="font-mono text-xs">{t.images}</TableCell>
                  <TableCell>
                    <Badge
                      variant={
                        t.status === "done"
                          ? "default"
                          : t.status === "error"
                            ? "destructive"
                            : "secondary"
                      }
                    >
                      {STATUS_LABEL[t.status] ?? t.status}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    {t.hasPrompt ? (
                      t.findings > 0 ? (
                        <Badge variant="destructive">{t.findings} 项错误</Badge>
                      ) : (
                        <Badge variant="secondary">通过</Badge>
                      )
                    ) : (
                      <span className="text-xs text-muted-foreground">—</span>
                    )}
                  </TableCell>
                  <TableCell className="text-xs text-muted-foreground">
                    {new Date(t.updatedAt).toLocaleString()}
                  </TableCell>
                  <TableCell>
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label="删除任务"
                      onClick={(e) => {
                        e.stopPropagation()
                        void remove(t.id)
                      }}
                    >
                      <Trash2Icon />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}
