package core

import (
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"netpilot/internal/lists"
	"netpilot/internal/model"
	"netpilot/internal/profiles"
)

// Plan — маршрутизация в нейтральном виде: узлы, группы, упорядоченные правила.
// Адаптеры внешних ядер (Xray, mihomo) переводят его в свой формат, поэтому
// логика «кто → что → куда» описана один раз.
type Plan struct {
	Nodes  []PlanNode
	Groups []PlanGroup
	// Proxy — что значит «через прокси» по умолчанию: тег узла или группы, "" — узлов нет.
	Proxy string
	Rules []PlanRule
	// Final — действие для трафика, не попавшего ни под одно правило.
	Final     string // "proxy" | "direct"
	BypassLAN bool
}

type PlanNode struct {
	Tag string
	model.Node
}

type PlanGroup struct {
	Tag     string
	Name    string
	Auto    bool     // true — быстрейший (url-test), false — выбор вручную
	Members []string // теги узлов
}

// PlanRule — правило. Внутри поля — «или», между полями — «и».
type PlanRule struct {
	Sources []string // CIDR устройств

	Domains  []string // точное совпадение
	Suffixes []string // домен и поддомены
	Keywords []string
	Regexps  []string
	Geosite  []string
	Geoip    []string // коды стран, нижний регистр
	CIDRs    []string
	// Скачанные списки (Re:filter): id → путь к текстовому файлу.
	DomainLists map[string]string
	IPLists     map[string]string

	Ports      []int
	PortRanges [][2]int
	Network    string   // "" | tcp | udp
	Protocols  []string // распознанный протокол: bittorrent

	Action string // proxy | direct | block
	Target string // тег узла или группы для proxy; "" — Plan.Proxy
}

func (r PlanRule) HasDomain() bool {
	return len(r.Domains)+len(r.Suffixes)+len(r.Keywords)+len(r.Regexps)+len(r.Geosite)+len(r.DomainLists) > 0
}

func (r PlanRule) HasIP() bool { return len(r.Geoip)+len(r.CIDRs)+len(r.IPLists) > 0 }

// SplitDomainIP делит правило на доменную и IP-часть (у многих ядер условия
// разных типов объединяются через «и», а нам нужно «или»).
func (r PlanRule) SplitDomainIP() (domain, ip *PlanRule) {
	if r.HasDomain() {
		d := r
		d.Geoip, d.CIDRs, d.IPLists = nil, nil, nil
		domain = &d
	}
	if r.HasIP() {
		i := r
		i.Domains, i.Suffixes, i.Keywords, i.Regexps, i.Geosite, i.DomainLists = nil, nil, nil, nil, nil, nil
		ip = &i
	}
	return
}

func NodeTag(id string) string { return "node-" + id }

// Автоматические группы: как часто проверять серверы и насколько текущий
// должен отстать от лучшего, чтобы группа на него переключилась. Большой
// допуск — переключение в основном при отказе сервера, а не из-за пинга.
const (
	AutoInterval    = "1m"
	AutoIntervalSec = 60
	AutoTolerance   = 500
)

func GroupTag(id string) string { return "group-" + id }

const AutoTag = "auto"

func BuildPlan(cfg *model.Config, env Env) (*Plan, error) {
	s := cfg.Settings
	p := &Plan{Final: "proxy", BypassLAN: s.BypassLAN}
	if s.DefaultAction == "direct" {
		p.Final = "direct"
	}
	var all []string
	for _, n := range cfg.Nodes {
		p.Nodes = append(p.Nodes, PlanNode{Tag: NodeTag(n.ID), Node: n})
		all = append(all, NodeTag(n.ID))
	}
	valid := slices.Clone(all)
	if len(all) > 1 {
		p.Groups = append(p.Groups, PlanGroup{Tag: AutoTag, Name: "Авто", Auto: true, Members: all})
		valid = append(valid, AutoTag)
	}
	for _, g := range cfg.Groups {
		members, err := groupMembers(cfg, g)
		if err != nil {
			return nil, err // в ошибке уже есть название группы
		}
		if len(members) == 0 {
			continue
		}
		p.Groups = append(p.Groups, PlanGroup{Tag: GroupTag(g.ID), Name: g.Name, Auto: g.Type != "select", Members: members})
		valid = append(valid, GroupTag(g.ID))
	}
	target := func(id string) string {
		for _, t := range []string{NodeTag(id), GroupTag(id)} {
			if id != "" && slices.Contains(valid, t) {
				return t
			}
		}
		return ""
	}
	switch {
	case target(s.DefaultTarget) != "":
		p.Proxy = target(s.DefaultTarget)
	case len(all) > 1:
		p.Proxy = AutoTag
	case len(all) == 1:
		p.Proxy = all[0]
	}

	// Маршруты устройств важнее правил.
	for _, d := range cfg.Devices {
		src := DeviceSources(d, env)
		if len(src) == 0 || (d.Route != "proxy" && d.Route != "direct" && d.Route != "block") {
			continue
		}
		r := PlanRule{Sources: src, Action: d.Route}
		if d.Route == "proxy" {
			r.Target = target(d.RouteTarget)
		}
		p.Rules = append(p.Rules, r)
	}

	for _, rule := range cfg.Rules {
		if !rule.Enabled {
			continue
		}
		r, ok := planRule(cfg, rule, env)
		if !ok {
			continue
		}
		if r.Action == "proxy" {
			r.Target = target(rule.Where.Target)
		}
		p.Rules = append(p.Rules, r)
	}
	return p, nil
}

func planRule(cfg *model.Config, rule model.Rule, env Env) (PlanRule, bool) {
	r := PlanRule{Action: rule.Where.Action}
	if r.Action != "direct" && r.Action != "block" {
		r.Action = "proxy"
	}
	if w := rule.Who; len(w.DeviceIDs)+len(w.Tags)+len(w.CIDRs) > 0 {
		for _, d := range cfg.Devices {
			tagged := false
			for _, t := range d.Tags {
				tagged = tagged || slices.Contains(w.Tags, t)
			}
			if slices.Contains(w.DeviceIDs, d.ID) || tagged {
				r.Sources = append(r.Sources, DeviceSources(d, env)...)
			}
		}
		for _, c := range w.CIDRs {
			r.Sources = append(r.Sources, toCIDR(c))
		}
		r.Sources = compact(r.Sources)
		if len(r.Sources) == 0 {
			return r, false // устройства без IP: иначе правило сработало бы для всех
		}
	}
	w := rule.What
	for _, id := range w.Apps {
		if pr, ok := profiles.Get(id); ok {
			r.Suffixes = append(r.Suffixes, pr.Domains...)
			r.CIDRs = append(r.CIDRs, pr.IPs...)
		}
	}
	for _, d := range w.Domains {
		d = strings.TrimSpace(d)
		switch {
		case d == "":
		case strings.HasPrefix(d, "full:"):
			r.Domains = append(r.Domains, strings.TrimPrefix(d, "full:"))
		case strings.HasPrefix(d, "keyword:"):
			r.Keywords = append(r.Keywords, strings.TrimPrefix(d, "keyword:"))
		case strings.HasPrefix(d, "regexp:"):
			r.Regexps = append(r.Regexps, strings.TrimPrefix(d, "regexp:"))
		default:
			r.Suffixes = append(r.Suffixes, strings.TrimPrefix(strings.TrimPrefix(d, "domain:"), "*."))
		}
	}
	for _, ip := range w.IPs {
		r.CIDRs = append(r.CIDRs, toCIDR(ip))
	}
	for _, g := range w.Geosite {
		if g = strings.ToLower(strings.TrimSpace(g)); g != "" {
			r.Geosite = append(r.Geosite, g)
		}
	}
	for _, g := range w.Geoip {
		if g = strings.ToLower(strings.TrimSpace(g)); g != "" {
			r.Geoip = append(r.Geoip, g)
		}
	}
	for _, p := range w.Ports {
		p = strings.TrimSpace(p)
		if a, b, ok := strings.Cut(p, "-"); ok {
			x, e1 := strconv.Atoi(strings.TrimSpace(a))
			y, e2 := strconv.Atoi(strings.TrimSpace(b))
			if e1 == nil && e2 == nil {
				r.PortRanges = append(r.PortRanges, [2]int{x, y})
			}
		} else if n, err := strconv.Atoi(p); err == nil {
			r.Ports = append(r.Ports, n)
		}
	}
	if w.Network == "tcp" || w.Network == "udp" {
		r.Network = w.Network
	}
	r.Protocols = compact(w.Protocols)
	r.Domains, r.Suffixes, r.Keywords, r.Regexps = compact(r.Domains), compact(r.Suffixes), compact(r.Keywords), compact(r.Regexps)
	r.Geosite, r.Geoip, r.CIDRs = compact(r.Geosite), compact(r.Geoip), compact(r.CIDRs)

	found := 0
	for _, id := range w.Lists {
		l, ok := lists.Get(id)
		path := lists.TextPath(env.ListsDir, id)
		if !ok || env.ListsDir == "" || !lists.Ready(path) {
			continue
		}
		found++
		if l.Kind == "ip" {
			if r.IPLists == nil {
				r.IPLists = map[string]string{}
			}
			r.IPLists[id] = path
		} else {
			if r.DomainLists == nil {
				r.DomainLists = map[string]string{}
			}
			r.DomainLists[id] = path
		}
	}
	// Правило только из нескачанных списков пропускаем, иначе оно стало бы «весь трафик».
	other := len(r.Domains)+len(r.Suffixes)+len(r.Keywords)+len(r.Regexps)+len(r.Geosite)+len(r.Geoip)+len(r.CIDRs) > 0
	if len(w.Lists) > 0 && found == 0 && !other {
		return r, false
	}
	return r, true
}

func groupMembers(cfg *model.Config, g model.Group) ([]string, error) {
	ids, err := GroupNodes(cfg, g)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = NodeTag(id)
	}
	return out, nil
}

// GroupNodes — id серверов группы: явный список и/или фильтр по имени,
// за вычетом подошедших под «исключить». Пустые список и фильтр — все серверы.
// CheckGroup проверяет группу перед сохранением: ошибка в выражении
// сломала бы конфигурацию ядра целиком.
func CheckGroup(g *model.Group) error {
	g.Name = strings.TrimSpace(g.Name)
	if g.Name == "" {
		return fmt.Errorf("укажите название группы")
	}
	if g.Type != "select" {
		g.Type = "auto"
	}
	for _, f := range []struct{ what, src string }{{"фильтре", g.Filter}, {"исключении", g.Exclude}} {
		if f.src == "" {
			continue
		}
		if _, err := regexp.Compile("(?i)" + f.src); err != nil {
			return fmt.Errorf("ошибка в %s «%s»: %v", f.what, f.src, err)
		}
	}
	return nil
}

func GroupNodes(cfg *model.Config, g model.Group) ([]string, error) {
	var re, ex *regexp.Regexp
	var err error
	if g.Filter != "" {
		if re, err = regexp.Compile("(?i)" + g.Filter); err != nil {
			return nil, fmt.Errorf("группа «%s»: ошибка в фильтре: %w", g.Name, err)
		}
	}
	if g.Exclude != "" {
		if ex, err = regexp.Compile("(?i)" + g.Exclude); err != nil {
			return nil, fmt.Errorf("группа «%s»: ошибка в исключении: %w", g.Name, err)
		}
	}
	var out []string
	for _, n := range cfg.Nodes {
		in := (len(g.NodeIDs) == 0 && re == nil) || slices.Contains(g.NodeIDs, n.ID) || (re != nil && re.MatchString(n.Name))
		if in && (ex == nil || !ex.MatchString(n.Name)) {
			out = append(out, n.ID)
		}
	}
	return out, nil
}

// DeviceSources — адреса, с которых приходит трафик устройства: IPv4 из
// настроек и IPv6 из таблицы соседей (/128). Без IPv6 трафик устройства по
// IPv6 в прозрачном режиме шёл бы мимо его маршрута и правил. Локальные
// адреса (fe80::/10) из сети не выходят и ядру не нужны; неверные
// пропускаются — один такой сломал бы правило целиком.
func DeviceSources(d model.Device, env Env) []string {
	var out []string
	if d.IP != "" {
		out = append(out, toCIDR(d.IP))
	}
	for _, s := range env.DeviceIPv6[d.ID] {
		a, err := netip.ParseAddr(strings.TrimSpace(s))
		if err != nil || !a.Is6() || a.Is4In6() || a.Zone() != "" || a.IsLinkLocalUnicast() {
			continue
		}
		out = append(out, netip.PrefixFrom(a, 128).String())
	}
	return out
}

func toCIDR(s string) string {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		return s
	}
	if a, err := netip.ParseAddr(s); err == nil {
		return netip.PrefixFrom(a, a.BitLen()).String()
	}
	return s
}

func compact(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// PrivateCIDRs — локальные сети: трафик сюда всегда идёт напрямую, если включено.
var PrivateCIDRs = []string{
	"10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
	"192.168.0.0/16", "224.0.0.0/4", "::1/128", "fc00::/7", "fe80::/10",
}
