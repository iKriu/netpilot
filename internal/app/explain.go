package app

import (
	"bufio"
	"context"
	"net"
	"net/netip"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"netpilot/internal/lists"
	"netpilot/internal/model"
	"netpilot/internal/profiles"
)

// Explain — куда пойдёт адрес и почему. Повторяет порядок, в котором решает
// ядро: локальная сеть, маршрут устройства, правила сверху вниз, затем
// «трафик без правила».
type Explain struct {
	Host   string   `json:"host"`
	IPs    []string `json:"ips,omitempty"`
	Action string   `json:"action"`           // proxy | direct | block
	Target string   `json:"target,omitempty"` // для proxy: имя сервера или группы
	RuleID string   `json:"ruleId,omitempty"`
	Rule   string   `json:"rule,omitempty"` // какое правило сработало
	Reason string   `json:"reason"`
	// Notes — правила, которые проверить нельзя (категории, страны, протоколы):
	// они могли бы сработать раньше.
	Notes []string `json:"notes,omitempty"`
}

// ExplainRoute отвечает на вопрос «куда пойдёт этот сайт».
// source — IP устройства; "" — для любого устройства.
func (a *App) ExplainRoute(ctx context.Context, host, source string) Explain {
	cfg := a.Store.Get()
	host = normalizeHost(host)
	ex := Explain{Host: host}

	var ips []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		ips = []netip.Addr{ip}
	} else if host != "" {
		rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		addrs, _ := net.DefaultResolver.LookupNetIP(rctx, "ip", host)
		cancel()
		for _, ip := range addrs {
			ips = append(ips, ip.Unmap())
		}
	}
	for _, ip := range ips {
		ex.IPs = append(ex.IPs, ip.String())
	}

	proxyTo := func(target string) {
		ex.Action = "proxy"
		ex.Target = targetLabel(cfg, target)
	}

	if cfg.Settings.BypassLAN && len(ips) > 0 && ips[0].IsPrivate() {
		ex.Action, ex.Reason = "direct", "адрес в локальной сети"
		return ex
	}

	if source != "" {
		for _, d := range cfg.Devices {
			if d.IP != source || d.Route == "" {
				continue
			}
			ex.Reason = "маршрут устройства «" + d.Name + "»"
			switch d.Route {
			case "proxy":
				proxyTo(d.RouteTarget)
			default:
				ex.Action = d.Route
			}
			return ex
		}
	}

	dir := a.listsDir()
	for _, r := range cfg.Rules {
		if !r.Enabled {
			continue
		}
		if !whoMatches(cfg, r.Who, source) {
			continue
		}
		w := r.What
		if len(w.Geosite)+len(w.Geoip)+len(w.Protocols) > 0 && !whatHasPlain(w) {
			// Протокол (BitTorrent) — свойство соединения, а не сайта: для сайта такое правило не срабатывает.
			if len(w.Geosite)+len(w.Geoip) > 0 {
				ex.Notes = append(ex.Notes, "правило «"+r.Name+"» проверяет категорию сайтов или страну — здесь его не проверить")
			}
			continue
		}
		if len(w.Ports) > 0 || w.Network != "" {
			// Без порта и протокола соединения правило с ними может как сработать, так и нет.
			ex.Notes = append(ex.Notes, "правило «"+r.Name+"» зависит от порта или протокола соединения")
			continue
		}
		if !whatMatches(w, host, ips, dir) {
			if len(w.Geosite)+len(w.Geoip) > 0 {
				ex.Notes = append(ex.Notes, "правило «"+r.Name+"» могло бы сработать по категории сайтов или стране")
			}
			continue
		}
		ex.RuleID, ex.Rule = r.ID, r.Name
		ex.Reason = "правило «" + r.Name + "»"
		switch r.Where.Action {
		case "proxy":
			proxyTo(r.Where.Target)
		default:
			ex.Action = r.Where.Action
		}
		return ex
	}

	ex.Reason = "ни одно правило не подошло"
	if cfg.Settings.DefaultAction == "direct" {
		ex.Action = "direct"
	} else {
		proxyTo("")
	}
	return ex
}

func normalizeHost(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	s, _, _ = strings.Cut(s, "/")
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	return strings.TrimSuffix(strings.TrimPrefix(s, "www."), ".")
}

// targetLabel — имя сервера или группы; "" — активный сервер.
func targetLabel(cfg *model.Config, id string) string {
	if id == "" {
		id = cfg.Settings.DefaultTarget
		if id == "" {
			return "Авто — все серверы"
		}
	}
	for _, g := range cfg.Groups {
		if g.ID == id {
			return g.Name
		}
	}
	for _, n := range cfg.Nodes {
		if n.ID == id {
			return n.Name
		}
	}
	return "Авто — все серверы"
}

func whoMatches(cfg *model.Config, who model.Who, source string) bool {
	if len(who.DeviceIDs)+len(who.Tags)+len(who.CIDRs) == 0 {
		return true
	}
	if source == "" {
		return false // правило для отдельных устройств, а устройство не выбрано
	}
	for _, d := range cfg.Devices {
		if d.IP != source {
			continue
		}
		if slices.Contains(who.DeviceIDs, d.ID) {
			return true
		}
		for _, t := range d.Tags {
			if slices.Contains(who.Tags, t) {
				return true
			}
		}
	}
	ip, err := netip.ParseAddr(source)
	if err != nil {
		return false
	}
	for _, c := range who.CIDRs {
		if prefixContains(c, ip) {
			return true
		}
	}
	return false
}

func whatHasPlain(w model.What) bool {
	return len(w.Apps)+len(w.Lists)+len(w.Domains)+len(w.IPs) > 0
}

func whatMatches(w model.What, host string, ips []netip.Addr, listsDir string) bool {
	if !whatHasPlain(w) && len(w.Geosite)+len(w.Geoip)+len(w.Protocols) == 0 {
		return true // «весь трафик»
	}
	for _, id := range w.Apps {
		p, ok := profiles.Get(id)
		if !ok {
			continue
		}
		if slices.ContainsFunc(p.Domains, func(d string) bool { return suffixMatch(host, d) }) {
			return true
		}
		if anyIPIn(ips, p.IPs) {
			return true
		}
	}
	for _, d := range w.Domains {
		d = strings.ToLower(strings.TrimSpace(d))
		switch {
		case d == "":
		case strings.HasPrefix(d, "full:"):
			if host == strings.TrimPrefix(d, "full:") {
				return true
			}
		case strings.HasPrefix(d, "keyword:"):
			if strings.Contains(host, strings.TrimPrefix(d, "keyword:")) {
				return true
			}
		case strings.HasPrefix(d, "regexp:"):
			if re, err := regexp.Compile(strings.TrimPrefix(d, "regexp:")); err == nil && re.MatchString(host) {
				return true
			}
		default:
			if suffixMatch(host, strings.TrimPrefix(strings.TrimPrefix(d, "domain:"), "*.")) {
				return true
			}
		}
	}
	if anyIPIn(ips, w.IPs) {
		return true
	}
	for _, id := range w.Lists {
		l, ok := lists.Get(id)
		if !ok {
			continue
		}
		set := loadListSet(lists.TextPath(listsDir, id))
		if set == nil {
			continue
		}
		if l.Kind == "ip" {
			if anyIPIn(ips, set.cidrs) {
				return true
			}
			continue
		}
		for h := host; h != ""; {
			if set.domains[h] {
				return true
			}
			_, rest, ok := strings.Cut(h, ".")
			if !ok {
				break
			}
			h = rest
		}
	}
	return false
}

func suffixMatch(host, d string) bool {
	d = strings.ToLower(strings.TrimSpace(d))
	return d != "" && (host == d || strings.HasSuffix(host, "."+d))
}

func anyIPIn(ips []netip.Addr, cidrs []string) bool {
	for _, ip := range ips {
		for _, c := range cidrs {
			if prefixContains(c, ip) {
				return true
			}
		}
	}
	return false
}

func prefixContains(c string, ip netip.Addr) bool {
	c = strings.TrimSpace(c)
	if p, err := netip.ParsePrefix(c); err == nil {
		return p.Contains(ip)
	}
	if a, err := netip.ParseAddr(c); err == nil {
		return a == ip
	}
	return false
}

// Скачанные списки читаются один раз и обновляются, когда меняется файл.
type listSet struct {
	mod     time.Time
	domains map[string]bool
	cidrs   []string
}

var (
	listCacheMu sync.Mutex
	listCache   = map[string]*listSet{}
)

func loadListSet(path string) *listSet {
	st, err := os.Stat(path)
	if err != nil {
		return nil
	}
	listCacheMu.Lock()
	defer listCacheMu.Unlock()
	if c := listCache[path]; c != nil && c.mod.Equal(st.ModTime()) {
		return c
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	s := &listSet{mod: st.ModTime(), domains: map[string]bool{}}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.ToLower(strings.TrimSpace(sc.Text()))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.ContainsAny(line, "/:") || net.ParseIP(line) != nil {
			s.cidrs = append(s.cidrs, line)
		} else {
			s.domains[strings.TrimPrefix(line, ".")] = true
		}
	}
	listCache[path] = s
	return s
}
