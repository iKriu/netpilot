// Package hiddify — ядро Hiddify Core (отдельный процесс). Это sing-box
// с дополнениями Hiddify; конфиг тот же, что у встроенного sing-box, поэтому
// работают Clash API, автоматические группы, списки и распознавание торрентов.
package hiddify

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"netpilot/internal/core"
	"netpilot/internal/core/external"
	"netpilot/internal/core/singbox"
	"netpilot/internal/model"
)

type Core struct {
	dir   string
	proc  external.Process
	mu    sync.Mutex
	clash string
}

func New(dataDir string) *Core { return &Core{dir: filepath.Join(dataDir, "cores", "hiddify")} }

// На Windows ядро — библиотека hiddify-core.dll, а запускает её HiddifyCli.exe.
var binary = map[bool]string{true: "HiddifyCli.exe", false: "hiddify-core"}[runtime.GOOS == "windows"]

func (c *Core) Name() string    { return "hiddify" }
func (c *Core) Version() string { return external.InstalledVersion(c.dir, binary) }
func (c *Core) Available() bool { return c.Version() != "" }
func (c *Core) Running() bool   { return c.proc.Running() }

func (c *Core) ClashAPI() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.proc.Running() {
		return ""
	}
	return c.clash
}

func (c *Core) Stop() error {
	c.mu.Lock()
	c.clash = ""
	c.mu.Unlock()
	return c.proc.Stop()
}

func (c *Core) Capabilities() core.Capabilities {
	return core.Capabilities{
		Protocols:  []string{"vless", "vmess", "trojan", "shadowsocks", "hysteria2", "tuic"},
		Transports: []string{"tcp", "ws", "grpc", "http", "h2", "httpupgrade"},
		Security:   []string{"none", "tls", "reality"},
		Lists:      true,
		Detect:     []string{"bittorrent"},
		MemoryMB:   70,
	}
}

func (c *Core) Render(cfg *model.Config, env core.Env) ([]byte, error) {
	env.Compat113 = true // Hiddify Core 4.x собран на sing-box 1.13
	return singbox.Render(cfg, env)
}

func (c *Core) Start(cfg *model.Config, env core.Env) error {
	if !c.Available() {
		return fmt.Errorf("ядро Hiddify не установлено")
	}
	// Кэш (выбор в группах, правила) — в папке ядра, а не рядом со встроенным sing-box.
	env.CacheFile = filepath.Join("cores", "hiddify", "cache.db") // относительно папки данных
	data, err := c.Render(cfg, env)
	if err != nil {
		return err
	}
	path := filepath.Join(c.dir, "config.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	bin := filepath.Join(c.dir, binary)
	// srun — обычный запуск sing-box без надстроек Hiddify над конфигом.
	logPath := filepath.Join(env.DataDir, "core.log")
	if err := c.proc.Start(bin, []string{"srun", "-c", path}, c.dir, logPath); err != nil {
		return err
	}
	// Запускалка Hiddify может не завершиться сама, если ядро не приняло
	// конфиг, — считаем запуск удачным, только когда ответил Clash API.
	if env.ClashAddr != "" {
		deadline := time.Now().Add(15 * time.Second)
		for {
			if conn, err := net.DialTimeout("tcp", env.ClashAddr, 500*time.Millisecond); err == nil {
				conn.Close()
				break
			}
			if !c.proc.Running() || time.Now().After(deadline) {
				_ = c.proc.Stop()
				return fmt.Errorf("ядро Hiddify не запустилось: %s", logTail(logPath))
			}
			time.Sleep(250 * time.Millisecond)
		}
	}
	c.mu.Lock()
	c.clash = env.ClashAddr
	c.mu.Unlock()
	return nil
}

// logTail — последние строки журнала без цветовых кодов.
func logTail(path string) string {
	b, _ := os.ReadFile(path)
	s := strings.TrimSpace(ansi.ReplaceAllString(string(b), ""))
	if s == "" {
		return "журнал пуст"
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 4 {
		lines = lines[len(lines)-4:]
	}
	return strings.Join(lines, "\n")
}

// ansi — цветовые коды в журнале ядра.
var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func (c *Core) Install(ctx context.Context, dl external.Downloader) (string, error) {
	return external.Install(ctx, dl, release, c.dir)
}

func (c *Core) Uninstall() error { return external.Uninstall(c.dir) }

var release = external.Release{
	Repo: "hiddify/hiddify-core",
	Asset: func(string) string {
		name := map[string]string{
			"windows/amd64": "hiddify-lib-windows-amd64.tar.gz",
			"linux/amd64":   "hiddify-core-linux-amd64.tar.gz",
			"linux/arm64":   "hiddify-core-linux-arm64.tar.gz",
			"linux/arm":     "hiddify-core-linux-armv7.tar.gz",
			"linux/mipsle":  "hiddify-core-linux-mipsle-softfloat.tar.gz",
		}[runtime.GOOS+"/"+runtime.GOARCH]
		return name
	},
	Binary: binary,
	// Без hiddify-core.dll HiddifyCli.exe не запустится; libcronet нужна
	// для некоторых протоколов и подгружается ядром при старте.
	Extract: []string{"hiddify-core.dll", "libcronet.dll", "libcronet.so"},
}
