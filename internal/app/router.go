package app

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"netpilot/internal/model"
	"netpilot/internal/router"
)

// Настройки сети применяются «на пробу»: если за это время их не подтвердить
// (например, пропал доступ к панели), они откатываются сами.
func confirmTimeout() time.Duration {
	if s, err := strconv.Atoi(os.Getenv("NETPILOT_ROUTER_CONFIRM_SECONDS")); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	return 90 * time.Second
}

type routerState struct {
	mu      sync.Mutex
	active  model.Router // что сейчас применено (подтверждённое или на пробе)
	pending *routerPending
}

type routerPending struct {
	next, prev model.Router
	deadline   time.Time
	timer      *time.Timer
}

type RouterView struct {
	Config      model.Router     `json:"config"`
	PasswordSet bool             `json:"passwordSet"` // пароль провайдера сохранён (сам он не отдаётся)
	Backend     string           `json:"backend"`
	Support     router.Support   `json:"support"`
	Interfaces  []router.Iface   `json:"interfaces"`
	Status      router.WANStatus `json:"status"`
	Pending     int64            `json:"pending,omitempty"` // срок подтверждения (unix), 0 — нет
}

func (a *App) RouterView(ctx context.Context) RouterView {
	cfg := a.Store.Get().Router
	a.router.mu.Lock()
	var deadline int64
	if p := a.router.pending; p != nil {
		cfg, deadline = p.next, p.deadline.Unix()
	}
	a.router.mu.Unlock()
	pwSet := cfg.WAN.Password != ""
	cfg.WAN.Password = ""
	return RouterView{
		Config: cfg, PasswordSet: pwSet, Backend: a.Router.Name(), Support: a.Router.Check(),
		Interfaces: a.Router.Interfaces(), Status: a.Router.Status(ctx), Pending: deadline,
	}
}

// RouterApply применяет настройки на пробу и возвращает срок подтверждения.
func (a *App) RouterApply(next model.Router) (time.Time, error) {
	if sup := a.Router.Check(); next.Enabled && !sup.Available {
		return time.Time{}, errors.New("режим роутера недоступен на этой системе")
	}
	a.router.mu.Lock()
	defer a.router.mu.Unlock()

	confirmed := a.Store.Get().Router
	base := confirmed
	if a.router.pending != nil {
		base = a.router.pending.next
	}
	// Пароль в интерфейс не отдаётся; пустое поле означает «не менять».
	if next.WAN.Password == "" && next.WAN.Username == base.WAN.Username {
		next.WAN.Password = base.WAN.Password
	}
	if err := router.Validate(next, a.Router.Interfaces()); err != nil {
		return time.Time{}, err
	}

	prev := confirmed
	if a.router.pending != nil {
		a.router.pending.timer.Stop()
		prev = a.router.pending.prev // откатываться — к последнему подтверждённому
	}
	if err := a.Router.Apply(next, a.leases()); err != nil {
		if rerr := a.Router.Apply(prev, a.leases()); rerr != nil {
			log.Printf("возврат прежних настроек сети: %v", rerr)
		}
		a.router.active, a.router.pending = prev, nil
		return time.Time{}, err
	}
	a.router.active = next
	deadline := time.Now().Add(confirmTimeout())
	p := &routerPending{next: next, prev: prev, deadline: deadline}
	p.timer = time.AfterFunc(confirmTimeout(), func() { a.routerRollback(p) })
	a.router.pending = p
	a.routerChanged()
	return deadline, nil
}

// RouterConfirm сохраняет настройки, применённые на пробу.
func (a *App) RouterConfirm() error {
	a.router.mu.Lock()
	defer a.router.mu.Unlock()
	p := a.router.pending
	if p == nil {
		return errors.New("нет настроек, ожидающих подтверждения")
	}
	p.timer.Stop()
	if err := a.Store.Update(func(c *model.Config) error { c.Router = p.next; return nil }); err != nil {
		return err
	}
	a.router.pending = nil
	log.Print("настройки сети подтверждены")
	return nil
}

func (a *App) routerRollback(p *routerPending) {
	a.router.mu.Lock()
	defer a.router.mu.Unlock()
	if a.router.pending != p {
		return // уже подтверждено или заменено
	}
	log.Print("настройки сети не подтверждены — возвращаю прежние")
	if err := a.Router.Apply(p.prev, a.leases()); err != nil {
		log.Printf("возврат прежних настроек сети: %v", err)
	}
	a.router.active, a.router.pending = p.prev, nil
	a.routerChanged()
}

func (a *App) RouterReconnect() error { return a.Router.Reconnect() }

func (a *App) RouterLog() string {
	return tailText(filepath.Join(a.DataDir, "router", "wan.log"), 16<<10)
}

// StartRouter поднимает сохранённую конфигурацию при запуске программы.
func (a *App) StartRouter() {
	cfg := a.Store.Get().Router
	if !cfg.Enabled {
		return
	}
	a.router.mu.Lock()
	defer a.router.mu.Unlock()
	if err := a.Router.Apply(cfg, a.leases()); err != nil {
		log.Printf("режим роутера: %v", err)
		return
	}
	a.router.active = cfg
	log.Printf("режим роутера: подключение %s через %s", cfg.WAN.Proto, cfg.WAN.Interface)
}

func (a *App) StopRouter() {
	a.router.mu.Lock()
	defer a.router.mu.Unlock()
	if a.router.active.Enabled {
		_ = a.Router.Disable()
	}
}

// routerChanged: прозрачный режим должен перехватывать только трафик LAN-порта.
func (a *App) routerChanged() {
	go func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		if a.fwOn {
			if err := a.applyFirewallLocked(); err != nil {
				log.Printf("обновление перехвата: %v", err)
			}
		}
	}()
}

// routerLAN — LAN-порт, если режим роутера включён (для перехвата трафика).
func (a *App) routerLAN() []string {
	a.router.mu.Lock()
	defer a.router.mu.Unlock()
	if r := a.router.active; r.Enabled && r.LAN.Interface != "" {
		return []string{r.LAN.Interface}
	}
	return nil
}

func (a *App) routerEnabled() bool {
	a.router.mu.Lock()
	defer a.router.mu.Unlock()
	return a.router.active.Enabled
}

// leases — закреплённые адреса устройств для DHCP-сервера роутера.
func (a *App) leases() []router.Lease {
	var out []router.Lease
	for _, d := range a.Store.Get().Devices {
		if d.Pinned && d.MAC != "" && d.IP != "" {
			out = append(out, router.Lease{MAC: d.MAC, IP: d.IP, Name: d.Name})
		}
	}
	return out
}

func tailText(path string, n int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > n {
		_, _ = f.Seek(st.Size()-n, 0)
	}
	b := make([]byte, n)
	k, _ := f.Read(b)
	return string(b[:k])
}
