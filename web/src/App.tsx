import { useCallback, useEffect, useState } from "react"
import {
  FileVideoIcon,
  ListChecksIcon,
  SettingsIcon,
  TriangleAlertIcon,
} from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Toaster } from "@/components/ui/sonner"
import { api, getToken, setToken } from "@/lib/api"
import { SettingsView } from "@/views/settings"
import { TaskDetailView } from "@/views/task-detail"
import { TasksView } from "@/views/tasks"
import { WorkflowsView } from "@/views/workflows"

type Route =
  | { name: "workflows" }
  | { name: "tasks" }
  | { name: "task"; id: string }
  | { name: "settings" }

function parseHash(): Route {
  const hash = window.location.hash.replace(/^#\/?/, "")
  const parts = hash.split("/").filter(Boolean)
  if (parts[0] === "tasks" && parts[1]) return { name: "task", id: parts[1] }
  if (parts[0] === "tasks") return { name: "tasks" }
  if (parts[0] === "settings") return { name: "settings" }
  return { name: "workflows" }
}

export function navigate(path: string) {
  window.location.hash = path
}

export function App() {
  const [route, setRoute] = useState<Route>(parseHash)
  const [needsToken, setNeedsToken] = useState(false)
  const [tokenInput, setTokenInput] = useState("")
  const [version, setVersion] = useState("")

  useEffect(() => {
    const onHash = () => setRoute(parseHash())
    window.addEventListener("hashchange", onHash)
    return () => window.removeEventListener("hashchange", onHash)
  }, [])

  const checkHealth = useCallback(async () => {
    try {
      const h = await api.health()
      setVersion(h.version)
      setNeedsToken(h.needsToken && !getToken())
    } catch {
      /* the shell still renders; individual views surface their own errors */
    }
  }, [])

  useEffect(() => {
    void checkHealth()
  }, [checkHealth])

  const nav = [
    { key: "workflows", label: "工作流", icon: FileVideoIcon, path: "/" },
    { key: "tasks", label: "任务", icon: ListChecksIcon, path: "/tasks" },
    { key: "settings", label: "设置", icon: SettingsIcon, path: "/settings" },
  ] as const

  const activeKey = route.name === "task" ? "tasks" : route.name

  return (
    <div className="flex min-h-svh flex-col bg-background">
      <header className="sticky top-0 z-10 border-b bg-background/95 backdrop-blur">
        <div className="mx-auto flex w-full max-w-7xl flex-wrap items-center gap-3 px-4 py-3">
          <div className="flex items-center gap-2">
            <span
              aria-hidden
              className="flex size-6 items-center justify-center rounded-md bg-primary text-[10px] font-bold tracking-tight text-primary-foreground"
            >
              H3
            </span>
            <span className="text-sm font-semibold">MiniMax H3 提示词助手</span>
            {version ? (
              <Badge variant="outline" className="font-mono">
                {version}
              </Badge>
            ) : null}
          </div>
          <nav className="flex items-center gap-1">
            {nav.map((item) => (
              <Button
                key={item.key}
                variant={activeKey === item.key ? "secondary" : "ghost"}
                size="sm"
                onClick={() => navigate(item.path)}
              >
                <item.icon data-icon="inline-start" />
                {item.label}
              </Button>
            ))}
          </nav>
        </div>
      </header>

      <main className="mx-auto w-full max-w-7xl flex-1 px-4 py-6">
        {route.name === "workflows" && <WorkflowsView />}
        {route.name === "tasks" && <TasksView />}
        {route.name === "task" && <TaskDetailView id={route.id} />}
        {route.name === "settings" && <SettingsView />}
      </main>

      <Dialog open={needsToken} onOpenChange={setNeedsToken}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <TriangleAlertIcon />
              需要访问令牌
            </DialogTitle>
            <DialogDescription>
              服务端配置了访问令牌，请输入后继续。令牌只保存在这台浏览器上。
            </DialogDescription>
          </DialogHeader>
          <Input
            value={tokenInput}
            onChange={(e) => setTokenInput(e.target.value)}
            placeholder="访问令牌"
            autoFocus
          />
          <DialogFooter>
            <Button
              onClick={() => {
                setToken(tokenInput.trim())
                setNeedsToken(false)
                window.location.reload()
              }}
            >
              保存并继续
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Toaster position="top-center" />
    </div>
  )
}

export default App
