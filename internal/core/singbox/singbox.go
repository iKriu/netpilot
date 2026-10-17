// Package singbox — встроенное ядро sing-box: работает внутри нашего
// процесса как библиотека, без отдельного бинарника.
package singbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json"

	"netpilot/internal/core"
	"netpilot/internal/core/logfile"
	"netpilot/internal/model"
)

type Core struct {
	mu     sync.Mutex
	box    *box.Box
	cancel context.CancelFunc
	clash  core.Clash
	gen    int // номер экземпляра: у соседних разные файлы кэша
	// stopTrim останавливает слежение за размером журнала.
	stopTrim chan struct{}

	// Прежний экземпляр, который дорабатывает начатые соединения.
	draining *drainingBox
}

type drainingBox struct {
	once   sync.Once
	box    *box.Box
	cancel context.CancelFunc
}

func (d *drainingBox) close() {
	d.once.Do(func() {
		_ = d.box.Close()
		d.cancel()
	})
}

func New() *Core { return &Core{} }

func (c *Core) Name() string    { return "sing-box" }
func (c *Core) Available() bool { return true }

func (c *Core) Version() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, d := range bi.Deps {
			if d.Path == "github.com/sagernet/sing-box" {
				return strings.TrimPrefix(d.Version, "v")
			}
		}
	}
	return "unknown"
}

func (c *Core) Capabilities() core.Capabilities {
	return core.Capabilities{
		Protocols:  []string{"vless", "vmess", "trojan", "shadowsocks", "hysteria2", "tuic"},
		Transports: []string{"tcp", "ws", "grpc", "http", "h2", "httpupgrade"},
		Security:   []string{"none", "tls", "reality"},
		Embedded:   true,
		Lists:      true,
		Detect:     []string{"bittorrent"},
		MemoryMB:   40,
	}
}

func (c *Core) Render(cfg *model.Config, env core.Env) ([]byte, error) {
	return Render(cfg, env)
}

func (c *Core) Start(cfg *model.Config, env core.Env) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.box != nil {
		return errors.New("ядро уже запущено")
	}
	logPath := filepath.Join(env.DataDir, "core.log")
	_ = os.Remove(logPath)
	_ = os.Remove(logPath + ".1")
	c.gen = 0
	env.CacheFile = "cache-0.db"
	instance, cancel, err := build(cfg, env)
	if err != nil {
		return err
	}
	c.box, c.cancel, c.clash = instance, cancel, core.Clash{Addr: env.ClashAddr, Secret: env.ClashSecret}
	c.stopTrim = make(chan struct{})
	go trimLog(logPath, c.stopTrim)
	return nil
}

// trimLog не даёт журналу расти без предела: sing-box сам пишет в файл
// (дописывая в конец), поэтому файл периодически обрезается со стороны.
func trimLog(path string, stop <-chan struct{}) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			_ = logfile.Trim(path, logfile.MaxSize)
		}
	}
}

// Reload применяет новую конфигурацию без остановки (только Linux): новый
// экземпляр ядра занимает те же порты через SO_REUSEPORT, и лишь после его
// успешного старта старый закрывается. Если новый не запустился, старый
// продолжает работать — сеть не пропадает из-за ошибки в настройках.
func (c *Core) Reload(cfg *model.Config, env core.Env) error {
	if runtime.GOOS != "linux" {
		return core.ErrReloadUnsupported
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.box == nil {
		return errors.New("ядро не запущено")
	}
	// Экземпляр от прошлого применения ещё дорабатывает соединения и держит
	// файл кэша, который нужен новому, — закрываем его сейчас.
	if c.draining != nil {
		c.draining.close()
		c.draining = nil
	}
	// Текущий экземпляр держит свой файл кэша, новому нужен другой.
	env.CacheFile = fmt.Sprintf("cache-%d.db", (c.gen+1)%2)
	instance, cancel, err := build(cfg, env)
	if err != nil {
		return err
	}
	old, oldCancel := c.box, c.cancel
	c.box, c.cancel, c.clash = instance, cancel, core.Clash{Addr: env.ClashAddr, Secret: env.ClashSecret}
	c.gen++
	// «Осушение»: у старого экземпляра закрываем только входы — новые
	// подключения теперь получает лишь новый, а начатые спокойно доработают.
	// Через drainTime старый закрывается целиком (долгие соединения
	// переподключатся уже к новому).
	for _, in := range old.Inbound().Inbounds() {
		_ = old.Inbound().Remove(in.Tag())
	}
	d := &drainingBox{box: old, cancel: oldCancel}
	c.draining = d
	time.AfterFunc(drainTime, d.close)
	return nil
}

// drainTime — сколько старый экземпляр дорабатывает начатые соединения.
const drainTime = 30 * time.Second

func build(cfg *model.Config, env core.Env) (*box.Box, context.CancelFunc, error) {
	data, err := Render(cfg, env)
	if err != nil {
		return nil, nil, err
	}
	// Итоговый конфиг сохраняется для отладки и сравнения с внешним sing-box:
	// в sing-box.json — только запущенный, иначе после неудачного применения
	// файл показывал бы не то, что работает. Не запустившийся — рядом.
	failed := filepath.Join(env.DataDir, "sing-box.failed.json")
	instance, cancel, err := startBox(data)
	if err != nil {
		_ = os.WriteFile(failed, data, 0o600)
		return nil, nil, err
	}
	_ = os.WriteFile(filepath.Join(env.DataDir, "sing-box.json"), data, 0o600)
	_ = os.Remove(failed)
	return instance, cancel, nil
}

func startBox(data []byte) (*box.Box, context.CancelFunc, error) {
	ctx, cancel := context.WithCancel(context.Background())
	ctx = include.Context(ctx)
	opts, err := json.UnmarshalExtendedContext[option.Options](ctx, data)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	instance, err := box.New(box.Options{Context: ctx, Options: opts})
	if err != nil {
		cancel()
		return nil, nil, err
	}
	if err := instance.Start(); err != nil {
		_ = instance.Close()
		cancel()
		return nil, nil, err
	}
	return instance, cancel, nil
}

func (c *Core) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.draining != nil {
		c.draining.close()
		c.draining = nil
	}
	if c.stopTrim != nil {
		close(c.stopTrim)
		c.stopTrim = nil
	}
	if c.box == nil {
		return nil
	}
	err := c.box.Close()
	c.cancel()
	c.box, c.cancel, c.clash = nil, nil, core.Clash{}
	return err
}

func (c *Core) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.box != nil
}

func (c *Core) ClashAPI() core.Clash {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.clash
}
