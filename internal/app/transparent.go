package app

import (
	"errors"
	"log"
	"net/netip"
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
		// Неверный адрес (ручная правка конфига) сломал бы nft-набор целиком
		// и с ним перехват для всех — такие пропускаем.
		var addrs []string
		for _, ip := range a.deviceIPv6(d.ID) {
			if validAddr(ip) {
				addrs = append(addrs, ip)
			}
		}
		if validAddr(d.IP) {
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

// validAddr — непустой корректный IP-адрес (без зоны: nftables её не примет).
func validAddr(s string) bool {
	ip, err := netip.ParseAddr(s)
	return err == nil && ip.Zone() == ""
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
	a.dnsFallbackLocked(false) // DNS устройств снова отвечает ядро
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
	a.dnsFallbackLocked(true)
}

// dnsFallbackLocked включает или выключает запасной DNS. Пока ядро не работает
// (упало, зависло, остановлено в панели), трафик устройств после снятия
// перехвата идёт напрямую, но DNS у них указан на шлюз — и без ядра на порту 53
// никто не отвечает: у всей сети перестают открываться сайты. Запасной DNS
// пересылает такие запросы туда, куда ходит сама система (роутер, провайдер).
// В режиме роутера DNS отвечает наш dnsmasq — запасной там не нужен и мешал бы.
func (a *App) dnsFallbackLocked(on bool) {
	if runtime.GOOS != "linux" {
		return
	}
	if !on {
		if !a.dnsFB {
			return
		}
		if err := firewall.DNSFallback(""); err != nil {
			log.Printf("запасной DNS: %v", err)
			return
		}
		a.dnsFB = false
		log.Print("запасной DNS выключен: DNS устройств снова обслуживает ядро")
		return
	}
	if !transparent(a.Store.Get().Settings) || a.routerEnabled() {
		return
	}
	up := firewall.SystemDNS()
	if up == "" {
		log.Print("запасной DNS не включён: не найден DNS-сервер системы")
		return
	}
	if err := firewall.DNSFallback(up); err != nil {
		log.Printf("запасной DNS: %v", err)
		return
	}
	a.dnsFB = true
	log.Printf("запасной DNS включён: запросы устройств к шлюзу идут на %s", up)
}

// Shutdown — завершение программы: ядро останавливается, перехват и запасной
// DNS снимаются. Запасной DNS без работающей программы не оставляем: правила
// пережили бы её удаление и незаметно меняли бы DNS сети.
func (a *App) Shutdown() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	err := a.stopLocked()
	a.dnsFallbackLocked(false)
	return err
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
	_ = firewall.DNSFallback("") // запасной DNS прошлого запуска
}

func (a *App) Transparent() TransparentView {
	a.mu.Lock()
	defer a.mu.Unlock()
	v := TransparentView{Enabled: transparent(a.Store.Get().Settings), Status: firewall.Check(), Error: a.fwErr}
	v.Active = a.fwOn
	return v
}
