package app

import (
	"errors"
	"log"
	"net/netip"
	"runtime"
	"time"

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
	if a.fwOn {
		if err := firewall.Remove(); err != nil {
			log.Printf("снятие перехвата: %v", err)
		}
		a.fwOn = false
	}
	a.ensureFallbackLocked()
}

// tpState — служебное состояние прозрачного режима и проверки живучести.
type tpState struct {
	// hold — не включать запасной DNS: ядро сейчас же запустится снова
	// (применение настроек ядру без перезагрузки на лету — остановка и запуск)
	// или программа завершается. Иначе правило мигнуло бы на долю секунды.
	hold bool
	// fbSkip — последняя причина, по которой запасной DNS не включён (в журнал один раз).
	fbSkip string
	// healthRestarts — перезапуски ядра проверкой живучести (см. health.go).
	healthRestarts []time.Time
}

// Точки подмены для тестов: на Windows нет nftables, а в тестах — настоящей сети.
var (
	fallbackOS     = runtime.GOOS == "linux"
	setDNSFallback = firewall.DNSFallback
	localResolver  = firewall.LocalResolver
	systemDNS      = firewall.SystemDNS
	defaultGateway = func() string {
		if routes, ok := firewall.DefaultRoutes(); ok && len(routes) > 0 && routes[0].Gateway.IsValid() {
			return routes[0].Gateway.String()
		}
		return ""
	}
)

// ensureFallbackLocked включает запасной DNS, если ядро не работает — по любой
// причине: остановлено, упало, не запустилось при старте программы. Перехват
// при этом мог и не включаться ни разу (fwOn=false), а DNS устройств всё равно
// указан на шлюз — поэтому решение не зависит от fwOn.
func (a *App) ensureFallbackLocked() {
	if a.active != nil || a.tp.hold {
		return
	}
	// «Блокировать интернет при сбое»: перехват оставлен намеренно, DNS тоже не подменяем.
	if a.fwOn {
		return
	}
	a.dnsFallbackLocked(true)
}

// dnsFallbackLocked включает или выключает запасной DNS. Пока ядро не работает
// (упало, зависло, остановлено в панели), трафик устройств после снятия
// перехвата идёт напрямую, но DNS у них указан на шлюз — и без ядра на порту 53
// никто не отвечает: у всей сети перестают открываться сайты. Запасной DNS
// пересылает такие запросы туда, куда ходит сама система (роутер, провайдер).
// В режиме роутера DNS отвечает наш dnsmasq — запасной там не нужен и мешал бы.
//
// Только IPv4: устройства получают DNS шлюза по DHCP (IPv4), а IPv6-адрес
// шлюза как DNS раздаётся редко; для ip6 понадобилась бы своя таблица с
// IPv6-upstream, которого у системы может и не быть.
func (a *App) dnsFallbackLocked(on bool) {
	if !fallbackOS {
		return
	}
	if !on {
		// Не полагаемся только на флаг: если прошлое удаление не удалось или
		// правило осталось от другого пути, DNSFallback("") проверит таблицу
		// сам и удалит её, если она есть.
		if err := setDNSFallback(""); err != nil {
			log.Printf("запасной DNS: %v", err)
			return
		}
		if a.dnsFB {
			log.Print("запасной DNS выключен: DNS устройств снова обслуживает ядро")
		}
		a.dnsFB = false
		return
	}
	if !transparent(a.Store.Get().Settings) || a.routerEnabled() {
		return
	}
	if a.dnsFB {
		return
	}
	skip := func(why string) {
		if why != a.tp.fbSkip {
			log.Printf("запасной DNS не включён: %s", why)
			a.tp.fbSkip = why
		}
	}
	if why := localResolver(); why != "" {
		skip(why)
		return
	}
	up := fallbackUpstream(systemDNS(), defaultGateway(), firewall.IsLocalAddr)
	if up == "" {
		skip("не найден DNS-сервер")
		return
	}
	if err := setDNSFallback(up); err != nil {
		log.Printf("запасной DNS: %v", err)
		return
	}
	a.dnsFB, a.tp.fbSkip = true, ""
	log.Printf("запасной DNS включён: запросы устройств к шлюзу идут на %s", up)
}

// fallbackUpstream выбирает, куда пересылать DNS устройств, пока ядро не
// работает. Адреса самого сервера не годятся никогда: DNAT на себя — петля.
//  1. DNS системы — туда ходит сам сервер, это проверенный путь;
//  2. шлюз по умолчанию — почти всегда это роутер, а домашние роутеры
//     отвечают на DNS (пересылают провайдеру). Он ближе и знает локальные
//     имена, поэтому лучше публичного;
//  3. 1.1.1.1 — обычный DNS без шифрования: правило nftables умеет только
//     пересылать пакеты, DoT/DoH тут невозможны. Если и он недоступен, хуже
//     не станет: без запасного DNS у устройств имена не разрешаются вовсе.
func fallbackUpstream(system, gateway string, isLocal func(netip.Addr) bool) string {
	for _, c := range []string{system, gateway, "1.1.1.1"} {
		ip, err := netip.ParseAddr(c)
		if err != nil || !ip.Is4() || ip.IsLoopback() || ip.IsUnspecified() || (isLocal != nil && isLocal(ip)) {
			continue
		}
		return ip.String()
	}
	return ""
}

// Shutdown — завершение программы: ядро останавливается, перехват и запасной
// DNS снимаются. Запасной DNS без работающей программы не оставляем: правила
// пережили бы её удаление и незаметно меняли бы DNS сети.
func (a *App) Shutdown() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tp.hold = true // программа завершается — запасной DNS не включаем
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
