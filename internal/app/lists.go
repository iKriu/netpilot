package app

import (
	"context"
	"log"
	"path/filepath"
	"slices"
	"time"

	"netpilot/internal/lists"
)

// Списки обновляются раз в сутки: Re:filter публикует новые каждый день.
const listsMaxAge = 24 * time.Hour

func (a *App) listsDir() string { return filepath.Join(a.DataDir, "lists") }

// usedLists — списки, на которые ссылаются включённые правила.
func (a *App) usedLists() []lists.List {
	var ids []string
	for _, r := range a.Store.Get().Rules {
		if r.Enabled {
			ids = append(ids, r.What.Lists...)
		}
	}
	var out []lists.List
	for _, l := range lists.All() {
		if slices.Contains(ids, l.ID) {
			out = append(out, l)
		}
	}
	return out
}

// ensureLists скачивает недостающие и устаревшие списки. Вызывается до запуска
// ядра и без блокировки приложения: загрузка может занять десятки секунд.
// Ошибка не мешает запуску — правило без списка просто не действует.
func (a *App) ensureLists(ctx context.Context, maxAge time.Duration) (changed bool) {
	for _, l := range a.usedLists() {
		ok, err := lists.Update(ctx, a.download, a.listsDir(), l, maxAge)
		if err != nil {
			log.Printf("список: %v", err)
			a.setListError(l.ID, err.Error())
			continue
		}
		a.setListError(l.ID, "")
		if ok {
			log.Printf("список «%s» обновлён", l.Name)
			changed = true
		}
	}
	return changed
}

// UpdateLists — обновить списки сейчас (кнопка в интерфейсе).
func (a *App) UpdateLists(ctx context.Context) error {
	if a.ensureLists(ctx, 0) {
		return a.applyIfRunning()
	}
	return nil
}

// refreshLists — ежедневное обновление; если списки изменились, конфигурация
// применяется без остановки (sing-box на Linux) или перезапуском.
func (a *App) refreshLists(ctx context.Context) {
	if a.ensureLists(ctx, listsMaxAge) {
		if err := a.applyIfRunning(); err != nil {
			log.Printf("применение обновлённых списков: %v", err)
		}
	}
}

func (a *App) applyIfRunning() error {
	a.mu.Lock()
	running := a.active != nil
	a.mu.Unlock()
	if !running {
		return nil
	}
	return a.Apply()
}

func (a *App) Lists() []lists.Status {
	used := a.usedLists()
	var out []lists.Status
	for _, l := range lists.All() {
		s := lists.StatusOf(a.listsDir(), l)
		s.Error = a.listError(l.ID)
		if !slices.ContainsFunc(used, func(u lists.List) bool { return u.ID == l.ID }) && !s.Ready {
			s.Error = "" // не используется и не скачан — это не ошибка
		}
		out = append(out, s)
	}
	return out
}

func (a *App) setListError(id, msg string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.listErrs == nil {
		a.listErrs = map[string]string{}
	}
	a.listErrs[id] = msg
}

func (a *App) listError(id string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.listErrs[id]
}
