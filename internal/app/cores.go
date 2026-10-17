package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"strings"
	"sync"
	"time"

	"netpilot/internal/core/external"
)

// installable — ядро, которое ставится отдельно (Xray, mihomo).
type installable interface {
	Install(ctx context.Context, dl external.Downloader) (string, error)
	Uninstall() error
}

func (a *App) InstallCore(ctx context.Context, name string) (string, error) {
	c := a.Cores.Get(name)
	inst, ok := c.(installable)
	if !ok {
		return "", fmt.Errorf("ядро %q встроено и не требует установки", name)
	}
	a.mu.Lock()
	// Упавшее ядро, которое вот-вот перезапустится, тоже не трогаем.
	running := a.active == c || (a.restart.pending && a.restart.core == name)
	a.mu.Unlock()
	if running {
		return "", errors.New("ядро сейчас работает — остановите его или выберите другое, затем обновите")
	}
	v, err := inst.Install(ctx, a.download)
	if err != nil {
		return "", err
	}
	log.Printf("ядро %s %s установлено", name, v)
	a.Changed() // автовыбор ядра мог измениться
	return v, nil
}

func (a *App) UninstallCore(name string) error {
	c := a.Cores.Get(name)
	inst, ok := c.(installable)
	if !ok {
		return fmt.Errorf("ядро %q встроено и не удаляется", name)
	}
	a.mu.Lock()
	running := a.active == c || (a.restart.pending && a.restart.core == name)
	a.mu.Unlock()
	if running {
		return errors.New("ядро сейчас работает — сначала остановите его")
	}
	return inst.Uninstall()
}

func isInstallable(c any) bool {
	_, ok := c.(installable)
	return ok
}

// ---------- последние версии ядер ----------

// coreRepos — где ядра выпускаются. Встроенный sing-box обновляется вместе
// с NetPilot; его последняя версия показывается, чтобы было видно, отстаёт ли сборка.
var coreRepos = map[string]string{
	"sing-box": "SagerNet/sing-box",
	"xray":     "XTLS/Xray-core",
	"mihomo":   "MetaCubeX/mihomo",
	"hiddify":  "hiddify/hiddify-core",
}

type latestCache struct {
	mu       sync.Mutex
	at       time.Time
	versions map[string]string
	busy     bool
}

const latestTTL = 6 * time.Hour

// coreLatest отдаёт известные последние версии и, если они устарели,
// обновляет их в фоне: список ядер не ждёт GitHub.
func (a *App) coreLatest() map[string]string {
	lc := &a.latest
	lc.mu.Lock()
	defer lc.mu.Unlock()
	if !lc.busy && time.Since(lc.at) > latestTTL {
		lc.busy = true
		go a.refreshLatest()
	}
	return maps.Clone(lc.versions)
}

func (a *App) refreshLatest() {
	out := map[string]string{}
	for name, repo := range coreRepos {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		raw, err := a.download(ctx, "https://api.github.com/repos/"+repo+"/releases/latest", 1<<20)
		cancel()
		var rel struct {
			Tag string `json:"tag_name"`
		}
		if err == nil && json.Unmarshal(raw, &rel) == nil && rel.Tag != "" {
			out[name] = strings.TrimPrefix(rel.Tag, "v")
		}
	}
	lc := &a.latest
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.busy = false
	if len(out) == 0 {
		lc.at = time.Now().Add(-latestTTL + 10*time.Minute) // нет сети — повторим через 10 минут
		return
	}
	lc.versions, lc.at = out, time.Now()
}
