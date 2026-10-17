package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"netpilot/internal/model"
	"netpilot/internal/oui"
	"netpilot/internal/scan"
	"netpilot/internal/store"
)

// network — сведения о сети, которые меняются часто и поэтому живут
// только в памяти (на роутере постоянная запись изнашивает флеш).
type network struct {
	scanMu   sync.Mutex
	mu       sync.Mutex
	seen     map[string]time.Time // id устройства → когда видели
	seenIP   map[string]string    // id устройства → фактический IP при последней встрече
	v6       map[string][]string  // id устройства → его IPv6-адреса (из таблицы соседей)
	lastScan time.Time
	networks []string
	warnings []string
	scanning bool
	applying bool // идёт автоприменение после смены адресов (routingIPsChanged)
}

const onlineWindow = 3 * time.Minute

// DeviceView — устройство вместе с тем, что о нём известно сейчас.
type DeviceView struct {
	model.Device
	Online    bool   `json:"online"`
	LastSeen  int64  `json:"lastSeen,omitempty"`
	SeenIP    string `json:"seenIp,omitempty"` // отличается от IP, если закреплённое устройство получило другой адрес
	RandomMAC bool   `json:"randomMac"`
	// Client — устройство пользуется NetPilot: пускает через него трафик
	// сейчас или пускало за последние clientWindow.
	Client bool `json:"client"`
	// Configured — устройство настроено вручную (имя, закрепление, группы,
	// маршрут или правило): показывается среди устройств NetPilot всегда.
	Configured bool `json:"configured"`
	DeviceTraffic
}

const (
	clientWindow = 30 * 24 * time.Hour
	staleAfter   = 14 * 24 * time.Hour // столько без появления в сети — и забытое устройство удаляется
	persistEvery = 12 * time.Hour      // как часто сохранять SeenAt и LastUsed
)

type NetworkView struct {
	Devices     []DeviceView `json:"devices"`
	Environment string       `json:"environment"`
	DHCP        string       `json:"dhcp"`
	DHCPManaged bool         `json:"dhcpManaged"`
	Networks    []string     `json:"networks"`
	Warnings    []string     `json:"warnings"`
	LastScan    int64        `json:"lastScan,omitempty"`
	Scanning    bool         `json:"scanning"`
}

func (a *App) Network() NetworkView {
	cfg := a.Store.Get()
	traffic := a.TrafficBySource()
	active := a.activeSources()
	dhcpName, dhcpManaged := a.DHCP.Name(), a.DHCP.Managed()
	if a.routerEnabled() {
		dhcpName, dhcpManaged = "NetPilot (режим роутера)", true
	}
	n := &a.net
	n.mu.Lock()
	defer n.mu.Unlock()
	v := NetworkView{
		Environment: scan.Environment(),
		DHCP:        dhcpName,
		DHCPManaged: dhcpManaged,
		Networks:    n.networks,
		Warnings:    n.warnings,
		Scanning:    n.scanning,
		Devices:     make([]DeviceView, 0, len(cfg.Devices)),
	}
	if !n.lastScan.IsZero() {
		v.LastScan = n.lastScan.Unix()
	}
	for _, d := range cfg.Devices {
		dv := DeviceView{Device: d, RandomMAC: d.MAC != "" && scan.RandomMAC(d.MAC), Configured: shownConfigured(cfg, d), DeviceTraffic: traffic[d.IP]}
		if t, ok := active[d.IP]; (ok && d.IP != "" && time.Since(t) < clientWindow) || (d.LastUsed > 0 && time.Since(time.Unix(d.LastUsed, 0)) < clientWindow) {
			dv.Client = true
		}
		if t, ok := n.seen[d.ID]; ok {
			dv.LastSeen = t.Unix()
			dv.Online = time.Since(t) < onlineWindow
			if ip := n.seenIP[d.ID]; ip != d.IP {
				dv.SeenIP = ip
			}
		}
		v.Devices = append(v.Devices, dv)
	}
	return v
}

type ScanReport struct {
	Found   int `json:"found"`
	Added   int `json:"added"`
	Updated int `json:"updated"`
	Removed int `json:"removed"`
}

// ScanNetwork ищет устройства и объединяет их со списком.
func (a *App) ScanNetwork(ctx context.Context, active bool) (ScanReport, error) {
	n := &a.net
	// Одновременно идёт одно сканирование: второй запрос дождётся первого
	// и получит свежий результат (таблица ARP к тому времени уже заполнена).
	n.scanMu.Lock()
	defer n.scanMu.Unlock()
	n.mu.Lock()
	n.scanning = true
	n.mu.Unlock()
	defer func() {
		n.mu.Lock()
		n.scanning = false
		n.mu.Unlock()
	}()

	res, err := scan.Scan(ctx, active)
	n.mu.Lock()
	n.networks, n.warnings = res.Networks, res.Warnings
	n.mu.Unlock()
	if err != nil {
		return ScanReport{}, err
	}
	if res.Isolated {
		return ScanReport{}, nil
	}
	rep, err := a.merge(res.Hosts)
	if err == nil {
		a.updateIPv6(res.Neighbors6)
	}
	return rep, err
}

// merge сопоставляет найденные хосты с устройствами по MAC.
func (a *App) merge(hosts []scan.Host) (ScanReport, error) {
	rep := ScanReport{Found: len(hosts)}
	now := time.Now()
	seen := map[string]string{}
	var ipChanged bool
	var warnings []string

	// Где каждый MAC виден в этом сканировании: закрепление, владелец которого
	// сейчас на другом адресе, на деле не действует (адрес не зарезервирован
	// на роутере), и его адрес честно принадлежит тому, кто на нём сейчас.
	seenAt := make(map[string]string, len(hosts))
	for _, h := range hosts {
		seenAt[h.MAC] = h.IP
	}
	err := a.Store.Update(func(c *model.Config) error {
		rep.Added, rep.Updated, rep.Removed, ipChanged, warnings = 0, 0, 0, false, nil
		clear(seen)
		byMAC := map[string]int{}
		byIP := map[string]int{}
		for i, d := range c.Devices {
			if d.MAC != "" {
				byMAC[scan.NormalizeMAC(d.MAC)] = i
			} else if d.IP != "" {
				byIP[d.IP] = i // добавлено вручную без MAC
			}
		}
		for _, h := range hosts {
			i, ok := byMAC[h.MAC]
			if !ok {
				if j, ok2 := byIP[h.IP]; ok2 {
					i, ok = j, true
					c.Devices[i].MAC = h.MAC
					rep.Updated++
				}
			}
			if !ok {
				vendor := oui.Vendor(h.MAC)
				c.Devices = append(c.Devices, model.Device{
					ID:        store.NewID(),
					Name:      defaultName(h, vendor),
					Type:      guessType(h, vendor),
					Vendor:    vendor,
					IP:        h.IP,
					MAC:       h.MAC,
					Hostname:  h.Hostname,
					Tags:      []string{},
					FirstSeen: now.Unix(),
				})
				i = len(c.Devices) - 1
				byMAC[h.MAC] = i
				rep.Added++
			}
			d := &c.Devices[i]
			seen[d.ID] = h.IP
			// Производитель для устройств, найденных до появления базы.
			if d.Vendor == "" && d.MAC != "" {
				if v := oui.Vendor(d.MAC); v != "" {
					d.Vendor = v
					if d.Type == "" || d.Type == "other" {
						d.Type = guessType(h, v)
					}
					rep.Updated++
				}
			}
			// Адрес мог остаться за другим устройством, которое ушло из сети, а DHCP
			// отдал его этому. Маршруты и правила работают по IP: устаревший адрес
			// направил бы чужой трафик по чужим настройкам (гость без интернета,
			// посторонний мимо прокси) — снимаем его. Закреплённый адрес не трогаем.
			pinnedBy := -1
			for j := range c.Devices {
				o := &c.Devices[j]
				// Устройство, добавленное вручную без MAC, задано самим адресом:
				// сняв его, мы бы потеряли устройство.
				if j == i || o.IP != h.IP || o.MAC == "" {
					continue
				}
				if o.Pinned {
					if at, ok := seenAt[scan.NormalizeMAC(o.MAC)]; !ok || at == h.IP {
						pinnedBy = j // владелец не виден в другом месте — адрес за ним
					} else {
						warnings = append(warnings, fmt.Sprintf("Адрес %s закреплён за «%s», но оно сейчас на %s, а этот адрес занял %s: закрепление не действует — зарезервируйте адрес на роутере.", h.IP, o.Name, at, h.MAC))
					}
					continue
				}
				o.IP = ""
				rep.Updated++
				if usedInRouting(c, *o) {
					ipChanged = true
				}
			}
			want := h.IP
			if pinnedBy >= 0 {
				// Адрес закреплён за другим устройством: новичку его не присваиваем,
				// чтобы его настройки не легли на чужой адрес.
				want = ""
				p := c.Devices[pinnedBy]
				warnings = append(warnings, fmt.Sprintf("Адрес %s закреплён за «%s», но его занимает другое устройство (%s). Пока это так, к нему применяются настройки «%s» — проверьте закрепление адреса в DHCP.", h.IP, p.Name, h.MAC, p.Name))
			}
			// Незакреплённое устройство следует за своим адресом: правила продолжают работать.
			if !d.Pinned && d.IP != want {
				d.IP = want
				rep.Updated++
				if usedInRouting(c, *d) {
					ipChanged = true
				}
			}
			if h.Gateway != d.Gateway {
				d.Gateway = h.Gateway
				if d.Gateway && (d.Type == "" || d.Type == "other") {
					d.Type = "network"
				}
				rep.Updated++
			}
			if h.Hostname != "" && d.Hostname != h.Hostname {
				old := d.Hostname
				d.Hostname = h.Hostname
				if autoNamed(*d, old) {
					d.Name = prettyName(h.Hostname)
				}
				if d.Type == "" || d.Type == "other" {
					d.Type = guessType(h, d.Vendor)
				}
				rep.Updated++
			} else if d.Hostname != "" && autoNamed(*d, d.Hostname) && d.Name != prettyName(d.Hostname) {
				d.Name = prettyName(d.Hostname) // имя, заданное раньше без оформления
				rep.Updated++
			}
			if now.Sub(time.Unix(d.SeenAt, 0)) > persistEvery {
				d.SeenAt = now.Unix()
				rep.Updated++
			}
		}
		active := a.activeSources()
		kept := c.Devices[:0]
		for _, d := range c.Devices {
			if t, ok := active[d.IP]; ok && d.IP != "" && t.Sub(time.Unix(d.LastUsed, 0)) > persistEvery {
				d.LastUsed = t.Unix()
				rep.Updated++
			}
			if d.SeenAt == 0 { // устройства из прошлых версий: отсчёт с этого момента
				d.SeenAt = now.Unix()
				rep.Updated++
			}
			if forgotten(c, d, now) {
				rep.Removed++
				continue
			}
			kept = append(kept, d)
		}
		c.Devices = kept
		if rep.Added == 0 && rep.Updated == 0 && rep.Removed == 0 {
			return errNoChanges
		}
		return nil
	})
	if err != nil && !errors.Is(err, errNoChanges) {
		return rep, err
	}

	n := &a.net
	n.mu.Lock()
	if n.seen == nil {
		n.seen, n.seenIP = map[string]time.Time{}, map[string]string{}
	}
	for id, ip := range seen {
		n.seen[id], n.seenIP[id] = now, ip
	}
	for _, w := range warnings {
		if !slices.Contains(n.warnings, w) {
			n.warnings = append(slices.Clip(n.warnings), w) // срез мог уйти в Network()
		}
	}
	n.lastScan = now
	n.mu.Unlock()

	// Новый IP у устройства с маршрутом или в правиле нужно донести до ядра.
	// Остальные изменения на маршрутизацию не влияют.
	if ipChanged {
		a.routingIPsChanged()
	}
	return rep, nil
}

// routingIPsChanged доносит сменившиеся адреса устройств до перехвата и ядра.
// Это изменение сделал сканер, а не человек: если ждать «Применить», наборы
// nftables и правила ядра продолжат направлять по старым адресам чужие
// устройства. Поэтому перехват обновляется сразу, а изменение отмечается как
// автоматическое (subsChanged), чтобы не отключать автоприменение подписок.
// Конфигурацию ядра применяем сами, только если других неприменённых правок
// нет, и только на Linux: там sing-box перезагружается без разрыва соединений,
// а на Windows перезапуск оборвал бы их — там изменение ждёт пользователя.
func (a *App) routingIPsChanged() {
	a.subsChanged()
	a.mu.Lock()
	if a.fwOn {
		if err := a.applyFirewallLocked(); err != nil {
			log.Printf("обновление перехвата: %v", err)
		}
	}
	running, onlyAuto := a.active != nil, a.dirty && a.dirtyBySubs
	a.mu.Unlock()
	if !running || !onlyAuto || runtime.GOOS != "linux" {
		return
	}
	// В фоне: Apply может докачивать списки (до минуты на каждый), а сюда
	// приходим из сканирования, которое держит scanMu. Одновременно — одно
	// применение: следующее всё равно увидит самую свежую конфигурацию.
	n := &a.net
	n.mu.Lock()
	if n.applying {
		n.mu.Unlock()
		return
	}
	n.applying = true
	n.mu.Unlock()
	go func() {
		defer func() { n.mu.Lock(); n.applying = false; n.mu.Unlock() }()
		for {
			if err := a.Apply(); err != nil {
				log.Printf("применение новых адресов устройств: %v", err)
				return
			}
			// Пока шло применение, адреса могли смениться ещё раз.
			a.mu.Lock()
			again := a.active != nil && a.dirty && a.dirtyBySubs
			a.mu.Unlock()
			if !again {
				return
			}
		}
	}()
}

var errNoChanges = errors.New("без изменений")

// updateIPv6 запоминает IPv6-адреса устройств по MAC. Если они изменились,
// а перехват включён, правила обновляются сразу.
func (a *App) updateIPv6(neigh map[string][]string) {
	if neigh == nil {
		return
	}
	cfg := a.Store.Get()
	next := map[string][]string{}
	for _, d := range cfg.Devices {
		if addrs := neigh[scan.NormalizeMAC(d.MAC)]; d.MAC != "" && len(addrs) > 0 {
			sorted := slices.Clone(addrs)
			slices.Sort(sorted)
			next[d.ID] = sorted
		}
	}
	n := &a.net
	n.mu.Lock()
	prev := n.v6
	changed := !maps.EqualFunc(prev, next, slices.Equal[[]string])
	n.v6 = next
	n.mu.Unlock()
	if !changed {
		return
	}
	// Адреса устройства с маршрутом или в правиле попадают и в правила ядра,
	// поэтому им нужен тот же путь, что и новым IPv4 (перехват + применение).
	// У остальных устройств IPv6 меняется часто (временные адреса), а ядру
	// они не нужны — хватает обновления перехвата.
	for _, d := range cfg.Devices {
		if usedInRouting(cfg, d) && !slices.Equal(prev[d.ID], next[d.ID]) {
			a.routingIPsChanged()
			return
		}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.fwOn {
		if err := a.applyFirewallLocked(); err != nil {
			log.Printf("обновление перехвата (IPv6): %v", err)
		}
	}
}

// deviceIPv6 — известные IPv6-адреса устройства.
func (a *App) deviceIPv6(id string) []string {
	a.net.mu.Lock()
	defer a.net.mu.Unlock()
	return a.net.v6[id]
}

// usedInRouting — участвует ли устройство в маршрутизации: свой маршрут или правило.
// configured — у устройства есть настройки, заданные человеком: такое не удаляется само.
func configured(c *model.Config, d model.Device) bool {
	return d.Pinned || len(d.Tags) > 0 || usedInRouting(c, d) || !autoNamed(d, d.Hostname)
}

// shownConfigured — настроено человеком; роутер с именем по умолчанию не в счёт.
func shownConfigured(c *model.Config, d model.Device) bool {
	if d.Gateway && d.Name == "Шлюз "+d.IP && !d.Pinned && len(d.Tags) == 0 && !usedInRouting(c, d) {
		return false
	}
	return configured(c, d)
}

// forgotten — устройство давно не появлялось в сети, не пользовалось NetPilot
// и ничем не настроено: например, старый случайный MAC телефона.
func forgotten(c *model.Config, d model.Device, now time.Time) bool {
	last := max(d.SeenAt, d.LastUsed, d.FirstSeen)
	return last > 0 && now.Sub(time.Unix(last, 0)) > staleAfter && !configured(c, d)
}

// CleanupDevices удаляет устройства, которых сейчас нет в сети, которые не
// пользовались NetPilot за clientWindow и ничем не настроены.
func (a *App) CleanupDevices() (int, error) {
	n := &a.net
	n.mu.Lock()
	seen := maps.Clone(n.seen)
	n.mu.Unlock()
	active := a.activeSources()
	removed := 0
	err := a.Store.Update(func(c *model.Config) error {
		kept := c.Devices[:0]
		for _, d := range c.Devices {
			online := time.Since(seen[d.ID]) < onlineWindow
			used := d.LastUsed > 0 && time.Since(time.Unix(d.LastUsed, 0)) < clientWindow
			if t, ok := active[d.IP]; ok && d.IP != "" && time.Since(t) < clientWindow {
				used = true
			}
			if !online && !used && !configured(c, d) {
				removed++
				continue
			}
			kept = append(kept, d)
		}
		c.Devices = kept
		if removed == 0 {
			return errNoChanges
		}
		return nil
	})
	if errors.Is(err, errNoChanges) {
		err = nil
	}
	return removed, err
}

func usedInRouting(c *model.Config, d model.Device) bool {
	if d.Route != "" {
		return true
	}
	for _, r := range c.Rules {
		if slices.Contains(r.Who.DeviceIDs, d.ID) {
			return true
		}
		for _, t := range d.Tags {
			if slices.Contains(r.Who.Tags, t) {
				return true
			}
		}
	}
	return false
}

// DevicesChanged вызывается после правки устройств: закрепления уходят в DHCP.
func (a *App) DevicesChanged() {
	a.Changed()
	// Маршруты «напрямую» и «без интернета» в прозрачном режиме работают в ядре
	// Linux — обновляем их сразу, без перезапуска ядра прокси.
	a.mu.Lock()
	if a.fwOn {
		if err := a.applyFirewallLocked(); err != nil {
			log.Printf("обновление перехвата: %v", err)
		}
	}
	a.mu.Unlock()
	// В режиме роутера адреса раздаёт наш dnsmasq: закрепления — ему.
	if a.routerEnabled() {
		if err := a.Router.UpdateLeases(a.leases()); err != nil {
			log.Printf("DHCP роутера: %v", err)
		}
	}
	if !a.DHCP.Managed() {
		return
	}
	if err := a.DHCP.Sync(a.Store.Get().Devices); err != nil {
		log.Printf("DHCP: %v", err)
	}
}

// WatchNetwork раз в минуту читает таблицу ARP (без опроса сети):
// так отметка «в сети» и адреса остаются свежими.
func (a *App) WatchNetwork(ctx context.Context) {
	// Имена устройств из их DHCP-запросов. В режиме роутера порт 67 нужен
	// нашему dnsmasq, а имена берутся из его аренд.
	go scan.ListenDHCP(ctx, func() bool { return !a.routerEnabled() })
	if _, err := a.ScanNetwork(ctx, true); err != nil {
		log.Printf("сканирование сети: %v", err)
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, _ = a.ScanNetwork(ctx, false)
		}
	}
}

func defaultName(h scan.Host, vendor string) string {
	if h.Hostname != "" {
		return prettyName(h.Hostname)
	}
	mac := macTail(h.MAC)
	switch {
	case h.Gateway:
		return "Шлюз " + h.IP
	case vendor != "":
		return vendor + " " + mac
	}
	return "Устройство " + mac
}

func macTail(mac string) string {
	mac = strings.ToUpper(strings.ReplaceAll(mac, ":", ""))
	if len(mac) >= 6 {
		mac = mac[len(mac)-6:]
	}
	return mac
}

// autoNamed — имя устройства придумала программа, а не человек: его можно
// заменить, когда стало известно настоящее. oldHost — прежнее сетевое имя.
func autoNamed(d model.Device, oldHost string) bool {
	if d.Name == "" {
		return true
	}
	if oldHost != "" && (d.Name == oldHost || d.Name == prettyName(oldHost)) {
		return true
	}
	tail := " " + macTail(d.MAC)
	if d.MAC == "" || !strings.HasSuffix(d.Name, tail) {
		return false
	}
	prefix := strings.TrimSuffix(d.Name, tail)
	return prefix == "Устройство" || prefix == d.Vendor || prefix == oui.Vendor(d.MAC)
}

var (
	androidAuto = regexp.MustCompile(`(?i)^android[-_][0-9a-f]{6,}$`)
	brandGlued  = regexp.MustCompile(`(iPhone|iPad|MacBook|iMac|Galaxy)(\p{Lu})`) // iPhoneNikita
	brandCase   = strings.NewReplacer("iphone", "iPhone", "Iphone", "iPhone", "IPHONE", "iPhone", "ipad", "iPad", "Ipad", "iPad",
		"macbook", "MacBook", "Macbook", "MacBook", "imac", "iMac", "Imac", "iMac", "airpods", "AirPods", "Airpods", "AirPods")
)

// prettyName делает из сетевого имени читаемое: «Ivans-iPhone» → «Ivans iPhone»,
// «galaxy-s23» → «galaxy s23», «android-3f2a9c…» → «Android».
func prettyName(host string) string {
	n := strings.TrimSpace(host)
	if androidAuto.MatchString(n) {
		return "Android"
	}
	// Имена Windows (DESKTOP-7K2L9QF) оставляем как есть, остальные — без дефисов.
	if n != strings.ToUpper(n) {
		n = strings.NewReplacer("-", " ", "_", " ").Replace(n)
	}
	n = brandGlued.ReplaceAllString(brandCase.Replace(n), "$1 $2")
	n = strings.Join(strings.Fields(n), " ")
	if n == "" {
		return host
	}
	return n
}

var typeHints = []struct {
	re  *regexp.Regexp
	typ string
}{
	{regexp.MustCompile(`(?i)iphone|android|galaxy|pixel|redmi|xiaomi|huawei|honor|oneplus|phone|poco|realme`), "phone"},
	{regexp.MustCompile(`(?i)ipad|tab|kindle`), "tablet"},
	{regexp.MustCompile(`(?i)tv|bravia|webos|tizen|chromecast|roku|firestick|mibox|androidtv`), "tv"},
	{regexp.MustCompile(`(?i)playstation|ps[345]|xbox|nintendo|switch|steamdeck`), "console"},
	{regexp.MustCompile(`(?i)printer|epson|canon|brother|laserjet|^hp[a-z0-9-]*$|mfp`), "printer"},
	{regexp.MustCompile(`(?i)macbook|laptop|notebook|thinkpad|book`), "laptop"},
	{regexp.MustCompile(`(?i)desktop|^pc|^win|imac|workstation|nas|synology|server`), "computer"},
	{regexp.MustCompile(`(?i)esp|tasmota|shelly|yeelight|sonoff|tuya|smart|plug|bulb|cam|vacuum|roborock`), "iot"},
	{regexp.MustCompile(`(?i)router|openwrt|keenetic|mikrotik|tp-?link|archer|ap-|mesh|switch`), "network"},
}

// guessType угадывает тип: по имени (iPhone-Masha → телефон), затем по производителю.
func guessType(host scan.Host, vendor string) string {
	if host.Gateway {
		return "network"
	}
	for _, h := range typeHints {
		if host.Hostname != "" && h.re.MatchString(host.Hostname) {
			return h.typ
		}
	}
	if t := oui.TypeHint(vendor); t != "" {
		return t
	}
	return "other"
}
