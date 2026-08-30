// Command h3helper serves the MiniMax H3 prompt helper: a LAN web app that
// reads local ComfyUI workflows, looks at the reference images with a vision
// model, asks the questions the H3 format needs answered, and writes a prompt
// that is then checked against the format rules.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"h3helper/internal/api"
	"h3helper/internal/comfy"
	"h3helper/internal/config"
	"h3helper/internal/store"
	"h3helper/webui"
)

var version = "dev"

func main() {
	log.SetFlags(log.Ltime)

	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	defaultData := filepath.Join(home, ".local", "share", "h3-prompt-helper")

	dataDir := flag.String("data", defaultData, "数据目录（配置和任务 JSON 都放这里）")
	listen := flag.String("listen", "", "监听地址，覆盖配置文件，例如 0.0.0.0:8199")
	comfyRoot := flag.String("comfyui", "", "ComfyUI 根目录，覆盖配置文件")
	showVersion := flag.Bool("version", false, "打印版本后退出")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	if err := run(*dataDir, *listen, *comfyRoot, home); err != nil {
		log.Fatalf("启动失败: %v", err)
	}
}

func run(dataDir, listenOverride, comfyOverride, home string) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("创建数据目录: %w", err)
	}

	cfg, err := config.Load(filepath.Join(dataDir, "config.json"), home)
	if err != nil {
		return fmt.Errorf("读取配置: %w", err)
	}
	if listenOverride != "" || comfyOverride != "" {
		if err := cfg.Update(func(c *config.Config) {
			if listenOverride != "" {
				c.Listen = listenOverride
			}
			if comfyOverride != "" {
				c.ComfyUIRoot = comfyOverride
			}
		}); err != nil {
			return err
		}
	}

	st, err := store.New(filepath.Join(dataDir, "tasks"))
	if err != nil {
		return fmt.Errorf("打开任务目录: %w", err)
	}

	lib := comfy.NewLibrary(cfg.WorkflowSearchDirs(), cfg.InputDir())
	srv := api.New(cfg, lib, st, webui.Handler(), version)

	current := cfg.Snapshot()
	httpServer := &http.Server{
		Addr:              current.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
	}

	ln, err := net.Listen("tcp", current.Listen)
	if err != nil {
		return fmt.Errorf("监听 %s: %w", current.Listen, err)
	}

	log.Printf("H3 提示词助手 %s", version)
	log.Printf("数据目录   %s", dataDir)
	switch {
	case cfg.Created():
		log.Printf("配置文件   %s（首次运行，已写入默认配置）", cfg.Path())
	case len(cfg.Filled()) > 0:
		log.Printf("配置文件   %s（已补全 %s）", cfg.Path(), strings.Join(cfg.Filled(), "、"))
	default:
		log.Printf("配置文件   %s", cfg.Path())
	}
	log.Printf("ComfyUI    %s", current.ComfyUIRoot)
	for _, dir := range cfg.WorkflowSearchDirs() {
		log.Printf("工作流目录 %s", dir)
	}
	for _, addr := range localURLs(current.Listen) {
		log.Printf("访问地址   %s", addr)
	}
	if current.Vision.Model == "" {
		log.Printf("提示：还没填视觉模型，分析参考图和生成提示词都会失败。在设置页填好接口地址和模型名，或直接改 %s。", cfg.Path())
	}
	if current.Token == "" {
		log.Printf("提示：当前没有设置访问令牌，局域网内任何人都能打开。可在设置页里配置。")
	}

	errCh := make(chan error, 1)
	go func() {
		if err := httpServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case <-stop:
		log.Println("正在退出…")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(ctx)
	}
}

// localURLs turns a bind address into the URLs that actually reach the server,
// so the log shows something usable from another machine on the LAN.
func localURLs(addr string) []string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return []string{"http://" + addr}
	}
	if host != "" && host != "0.0.0.0" && host != "::" {
		return []string{fmt.Sprintf("http://%s:%s", host, port)}
	}

	out := []string{fmt.Sprintf("http://127.0.0.1:%s", port)}
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}
			out = append(out, fmt.Sprintf("http://%s:%s", ipnet.IP.String(), port))
		}
	}
	return out
}
