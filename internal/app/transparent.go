package app

import (
	"errors"
	"log"
	"runtime"

	"netpilot/internal/firewall"
	"netpilot/internal/model"
	"netpilot/internal/scan"
)

// TransparentView — состояние прозрачного режима для интерфейса.
type TransparentView struct {
	Enabled bool `json:"enabled"` // включён в настройках
	firewall.Status
	Error string `json:"error,omitempty"`
}

func transparent(s model.Settings) bool { return s.Mode == "transparent" }

func (a *App) firewallSpec(cfg *model.Config) firewall.Spec {
	s := cfg.Settings
	spec := firewall.Spec{
		Port: s.TProxyPort, Scope: s.TransparentScope, HijackDNS: s.HijackDNS,
		IPv6: s.IPv6, LAN6: scan.LAN6Prefixes(),
		// В режиме роутера перехватываем только трафик из локальной сети.
		LanIfaces: a.routerLAN(),
	}
	// Без режима роутера NetPilot — шлюз «на одной ноге» внутри сети роутера:
	// трафик мимо прокси маскируется, чтобы ответы шли обратно через шлюз.
	// В режиме роутера NAT делает сам режим роутера.
	spec.Masquerade = len(spec.LanIfaces) == 0
	if spec.IPv6 == "" {
		spec.IPv6 = firewall.IPv6Block
	}
	for _, d := range cfg.Devices {
		// IPv4 из настроек и IPv6 из таблицы соседей: маршрут действует на оба.
		addrs := append([]string(nil), a.deviceIPv6(d.ID)...)
		if d.IP != "" {
			addrs = append(addrs, d.IP)
		}
		switch d.Route {
		case "direct":
			spec.Direct = append(spec.Direct, addrs...)
		case "block":
			spec.Block = append(spec.Block, addrs...)
		case "proxy":
			spec.Proxy = append(spec.Proxy, addrs...)
		}
	}
	return spec
}

// applyFirewallLocked включает или обновляет перехват по текущей конфигурации.
func (a *App) applyFirewallLocked() error {
	cfg := a.Store.Get()
	if !transparent(cfg.Settings) || a.active == nil {
		return nil
	}
	if runtime.GOOS != "linux" {
		return errors.New("прозрачный режим работает только на Linux (OpenWrt, ZimaOS и другие)")
	}
	if cfg.Settings.TProxyPort <= 0 {
		return errors.New("не задан порт TPROXY")
	}
	if err := firewall.Apply(a.firewallSpec(cfg)); err != nil {
		a.fwErr = err.Error()
		return err
	}
	a.fwErr, a.fwOn = "", true
	return nil
}

func (a *App) removeFirewallLocked() {
	if !a.fwOn {
		return
	}
	if err := firewall.Remove(); err != nil {
		log.Printf("снятие перехвата: %v", err)
	}
	a.fwOn = false
}

// cleanupFirewall убирает перехват, оставшийся от аварийно завершённого запуска.
func cleanupFirewall() {
	if runtime.GOOS != "linux" {
		return
	}
	if st := firewall.Check(); st.Active {
		log.Print("найдены правила перехвата от прошлого запуска — удаляю")
		if err := firewall.Remove(); err != nil {
			log.Printf("снятие перехвата: %v", err)
		}
	}
}

func (a *App) Transparent() TransparentView {
	a.mu.Lock()
	defer a.mu.Unlock()
	v := TransparentView{Enabled: transparent(a.Store.Get().Settings), Status: firewall.Check(), Error: a.fwErr}
	v.Active = a.fwOn
	return v
}
