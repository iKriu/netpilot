package singbox

import (
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"net/netip"
	"net/url"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"netpilot/internal/core"
	"netpilot/internal/lists"
	"netpilot/internal/model"
	"netpilot/internal/profiles"
)

type M = map[string]any

const (
	tagDirect = "direct"
	tagProxy  = "proxy"
	tagAuto   = "auto"
	tagIn     = "mixed-in"
	tagTProxy = "tproxy-in"

	dnsLocal  = "dns-local"
	dnsDirect = "dns-direct"
	dnsRemote = "dns-remote"

	httpDirect = "http-direct"
)

func nodeTag(id string) string  { return "node-" + id }
func groupTag(id string) string { return "group-" + id }

// Render переводит единую модель в конфиг sing-box.
func Render(cfg *model.Config, env core.Env) ([]byte, error) {
	return render(cfg, env, false)
}

// RenderExternal — конфиг для sing-box в отдельном процессе (Hiddify): журнал
// идёт в stderr, а в файл его пишет NetPilot с ограничением размера. Если бы
// процесс сам писал в core.log, файл рос бы без предела.
func RenderExternal(cfg *model.Config, env core.Env) ([]byte, error) {
	return render(cfg, env, true)
}

func render(cfg *model.Config, env core.Env, external bool) ([]byte, error) {
	s := cfg.Settings
	r := &renderer{cfg: cfg, ruleSets: map[string]M{}, listsDir: env.ListsDir, env: env}

	outbounds := []M{{"type": "direct", "tag": tagDirect}}
	for _, n := range cfg.Nodes {
		ob, err := nodeOutbound(n)
		if err != nil {
			return nil, fmt.Errorf("сервер «%s»: %w", n.Name, err)
		}
		outbounds = append(outbounds, ob)
		r.valid = append(r.valid, nodeTag(n.ID))
	}

	// Встроенная группа «Авто» по всем узлам — чтобы всё работало сразу после импорта.
	var selectable []string
	if len(cfg.Nodes) > 1 {
		outbounds = append(outbounds, autoGroup(tagAuto, r.valid, env.Selected))
		selectable = append(selectable, tagAuto)
	}
	for _, g := range cfg.Groups {
		members, err := groupMembers(cfg, g)
		if err != nil {
			return nil, err // в ошибке уже есть название группы
		}
		if len(members) == 0 {
			continue // пустая группа невалидна для ядра, правила на неё уйдут в «proxy»
		}
		// И автоматическая, и ручная группа — переключатель; выбранный
		// сервер (NetPilot или человек) передаётся при каждом запуске ядра.
		tag := groupTag(g.ID)
		outbounds = append(outbounds, autoGroup(tag, members, env.Selected))
		selectable = append(selectable, tag)
		r.valid = append(r.valid, tag)
	}
	selectable = append(selectable, nodeTagsOf(cfg)...)
	if len(selectable) == 0 {
		selectable = []string{tagDirect} // узлов нет — «прокси» ведёт напрямую
	}
	proxy := M{"type": "selector", "tag": tagProxy, "outbounds": selectable}
	if t := r.targetTag(s.DefaultTarget); t != "" && slices.Contains(selectable, t) {
		proxy["default"] = t
	}
	outbounds = append(outbounds, proxy)

	rules := []M{
		{"action": "sniff"},
		{"protocol": "dns", "action": "hijack-dns"},
	}
	if s.BypassLAN {
		rules = append(rules, M{"ip_is_private": true, "action": "route", "outbound": tagDirect})
	}
	rules = append(rules, r.deviceRoutes()...)

	// Правила по IP/стране не сработают на запрос с доменом, пока домен не
	// разрешён. Поэтому правило делится на доменную часть (без DNS) и IP-часть,
	// а шаг resolve ставится только перед первой IP-частью: домен, совпавший
	// раньше, уходит сразу, без лишнего DNS-запроса.
	resolved := false
	for _, rule := range cfg.Rules {
		if !rule.Enabled {
			continue
		}
		sr, ok := r.rule(rule)
		if !ok {
			continue
		}
		domainPart, ipPart := splitByMatch(sr)
		if domainPart != nil {
			rules = append(rules, domainPart)
		}
		if ipPart != nil {
			if !resolved {
				rules = append(rules, M{"action": "resolve", "server": dnsDirect})
				resolved = true
			}
			rules = append(rules, ipPart)
		}
		if domainPart == nil && ipPart == nil {
			rules = append(rules, sr)
		}
	}

	final := tagProxy
	if s.DefaultAction == "direct" {
		final = tagDirect
	}

	route := M{
		"rules":                   rules,
		"final":                   final,
		"auto_detect_interface":   env.OS != "linux",
		"default_domain_resolver": M{"server": dnsDirect},
	}
	// Списки geosite/geoip качаются напрямую: иначе нерабочий узел
	// не дал бы ядру даже запуститься. В sing-box 1.14 — общий HTTP-клиент,
	// в 1.13 (Hiddify) — download_detour у каждого списка.
	if !env.Compat113 {
		route["default_http_client"] = httpDirect
	}
	if len(r.ruleSets) > 0 {
		var sets []M
		for _, name := range sortedKeys(r.ruleSets) {
			rs := r.ruleSets[name]
			if env.Compat113 && rs["type"] == "remote" {
				rs = maps.Clone(rs)
				rs["download_detour"] = tagDirect
			}
			sets = append(sets, rs)
		}
		route["rule_set"] = sets
	}

	remote, err := dnsServer(dnsRemote, s.DNSRemote)
	if err != nil {
		return nil, fmt.Errorf("DNS для прокси: %w", err)
	}
	remote["detour"] = tagProxy
	direct, err := dnsServer(dnsDirect, s.DNSDirect)
	if err != nil {
		return nil, fmt.Errorf("DNS для прямого трафика: %w", err)
	}

	inbound := M{
		"type":        "mixed",
		"tag":         tagIn,
		"listen":      orDefault(s.ListenAddr, "127.0.0.1"),
		"listen_port": s.MixedPort,
	}
	if s.SystemProxy && (env.OS == "windows" || env.OS == "darwin") {
		inbound["set_system_proxy"] = true
	}
	// На Linux reuse_addr включает SO_REUSEPORT: новый экземпляр ядра может
	// занять те же порты, пока старый ещё работает, — перезагрузка без отказов.
	if env.OS == "linux" {
		inbound["reuse_addr"] = true
	}

	inbounds := []M{inbound}
	if s.Mode == "transparent" && env.OS == "linux" {
		inbounds = append(inbounds, M{
			"type":        "tproxy",
			"tag":         tagTProxy,
			"listen":      "::",
			"listen_port": s.TProxyPort,
			"reuse_addr":  true,
		})
	}

	experimental := M{
		"cache_file": M{"enabled": true, "path": filepath.Join(env.DataDir, orDefault(env.CacheFile, "cache.db"))},
	}
	if env.ClashAddr != "" {
		experimental["clash_api"] = M{"external_controller": env.ClashAddr, "secret": env.ClashSecret}
	}

	conf := M{
		"log": M{
			"level":     orDefault(s.LogLevel, "info"),
			"output":    filepath.Join(env.DataDir, "core.log"),
			"timestamp": true,
		},
		"dns": M{
			"servers": []M{{"type": "local", "tag": dnsLocal}, direct, remote},
			"rules":   r.dnsRules(),
			"final":   dnsFinal(s),
			// Шлюз отвечает устройствам на DNS и запоминает, какому домену принадлежит
			// IP. Тогда и трафик без имени сайта — звонки Discord, UDP — попадает под
			// правила по доменам.
			"reverse_mapping": s.Mode == "transparent" && env.OS == "linux",
		},
		"inbounds":     inbounds,
		"outbounds":    outbounds,
		"route":        route,
		"experimental": experimental,
	}
	if external {
		conf["log"] = M{"level": orDefault(s.LogLevel, "info"), "timestamp": true, "disable_color": true}
	}
	if !env.Compat113 {
		conf["http_clients"] = []M{{"tag": httpDirect}} // без detour — прямое подключение
	}
	return json.MarshalIndent(conf, "", "  ")
}

type renderer struct {
	cfg      *model.Config
	valid    []string // теги узлов и групп, реально попавших в конфиг
	ruleSets map[string]M
	listsDir string
	env      core.Env
}

// listSet — локальный набор правил из скачанного списка ("" — списка нет или
// он ещё не скачан). Префикс тега говорит, доменный это список или IP.
func (r *renderer) listSet(id string) string {
	l, ok := lists.Get(id)
	if !ok || r.listsDir == "" {
		return ""
	}
	path := lists.SRSPath(r.listsDir, id)
	if !lists.Ready(path) {
		return ""
	}
	tag := "listd-" + id
	if l.Kind == "ip" {
		tag = "listip-" + id
	}
	r.ruleSets[tag] = M{"type": "local", "tag": tag, "format": "binary", "path": path}
	return tag
}

func (r *renderer) targetTag(id string) string {
	if id == "" {
		return ""
	}
	for _, t := range []string{nodeTag(id), groupTag(id)} {
		if slices.Contains(r.valid, t) {
			return t
		}
	}
	return ""
}

// dnsFinal — чем отвечать устройствам на остальные DNS-запросы. Через прокси —
// только по явной настройке: иначе нерабочий узел оставил бы без DNS всю сеть.
func dnsFinal(s model.Settings) string {
	if s.DNSViaProxy {
		return dnsRemote
	}
	return dnsDirect
}

// dnsRules: домены, которые по правилам идут напрямую, разрешаются прямым DNS —
// тогда CDN отдаёт ближайшие к пользователю серверы. Остальное — через прокси,
// чтобы провайдер не видел запросов. Условия по IP в DNS-правилах не используются:
// до ответа DNS адреса ещё нет.
func (r *renderer) dnsRules() []M {
	out := []M{}
	for _, rule := range r.cfg.Rules {
		if !rule.Enabled || rule.Where.Action != "direct" {
			continue
		}
		sr, ok := r.rule(rule)
		if !ok {
			continue
		}
		domainPart, _ := splitByMatch(sr)
		if domainPart == nil {
			continue
		}
		d := M{"action": "route", "server": dnsDirect}
		for _, k := range append(slices.Clone(domainKeys), "rule_set", "source_ip_cidr") {
			if v, ok := domainPart[k]; ok {
				d[k] = v
			}
		}
		out = append(out, d)
	}
	return out
}

// deviceRoutes — маршрут, выбранный для устройства целиком. Он важнее правил,
// поэтому идёт раньше них. Устройства с одинаковым маршрутом объединяются в одно правило.
func (r *renderer) deviceRoutes() []M {
	type key struct{ route, target string }
	groups := map[key][]string{}
	var order []key
	for _, d := range r.cfg.Devices {
		src := core.DeviceSources(d, r.env)
		if len(src) == 0 || (d.Route != "proxy" && d.Route != "direct" && d.Route != "block") {
			continue
		}
		k := key{d.Route, ""}
		if d.Route == "proxy" {
			k.target = r.targetTag(d.RouteTarget)
		}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], src...)
	}
	var out []M
	for _, k := range order {
		rule := M{"source_ip_cidr": compact(groups[k])}
		switch k.route {
		case "block":
			rule["action"] = "reject"
		case "direct":
			rule["action"], rule["outbound"] = "route", tagDirect
		default:
			rule["action"], rule["outbound"] = "route", orDefault(k.target, tagProxy)
		}
		out = append(out, rule)
	}
	return out
}

// rule переводит правило «Кто → Что → Куда». false — правило пропускается
// (например, выбраны устройства, но ни у одного нет IP: иначе оно сработало бы для всех).
func (r *renderer) rule(rule model.Rule) (M, bool) {
	out := M{}

	// Кто
	if len(rule.Who.DeviceIDs)+len(rule.Who.Tags)+len(rule.Who.CIDRs) > 0 {
		var src []string
		for _, d := range r.cfg.Devices {
			if slices.Contains(rule.Who.DeviceIDs, d.ID) || intersects(d.Tags, rule.Who.Tags) {
				src = append(src, core.DeviceSources(d, r.env)...)
			}
		}
		for _, c := range rule.Who.CIDRs {
			src = append(src, toCIDR(c))
		}
		src = compact(src)
		if len(src) == 0 {
			return nil, false
		}
		out["source_ip_cidr"] = src
	}

	// Что
	w := rule.What
	var domain, suffix, keyword, regex, cidr []string
	for _, id := range w.Apps {
		if p, ok := profiles.Get(id); ok {
			suffix = append(suffix, p.Domains...)
			cidr = append(cidr, p.IPs...)
		}
	}
	for _, d := range w.Domains {
		d = strings.TrimSpace(d)
		switch {
		case d == "":
		case strings.HasPrefix(d, "full:"):
			domain = append(domain, strings.TrimPrefix(d, "full:"))
		case strings.HasPrefix(d, "keyword:"):
			keyword = append(keyword, strings.TrimPrefix(d, "keyword:"))
		case strings.HasPrefix(d, "regexp:"):
			regex = append(regex, strings.TrimPrefix(d, "regexp:"))
		default:
			suffix = append(suffix, strings.TrimPrefix(strings.TrimPrefix(d, "domain:"), "*."))
		}
	}
	for _, ip := range w.IPs {
		cidr = append(cidr, toCIDR(ip))
	}
	setIf(out, "domain", compact(domain))
	setIf(out, "domain_suffix", compact(suffix))
	setIf(out, "domain_keyword", compact(keyword))
	setIf(out, "domain_regex", compact(regex))
	setIf(out, "ip_cidr", compact(cidr))

	var sets []string
	for _, g := range w.Geosite {
		sets = append(sets, r.ruleSet("geosite", g))
	}
	for _, g := range w.Geoip {
		sets = append(sets, r.ruleSet("geoip", g))
	}
	listsFound := 0
	for _, id := range w.Lists {
		if tag := r.listSet(id); tag != "" {
			sets = append(sets, tag)
			listsFound++
		}
	}
	// Правило только из списков, которые ещё не скачаны, пропускаем: иначе
	// оно стало бы «весь трафик».
	otherWhat := len(domain)+len(suffix)+len(keyword)+len(regex)+len(cidr)+len(w.Geosite)+len(w.Geoip)+len(w.Protocols) > 0
	if len(w.Lists) > 0 && listsFound == 0 && !otherWhat {
		return nil, false
	}
	setIf(out, "rule_set", compact(sets))

	var ports []int
	var ranges []string
	for _, p := range w.Ports {
		p = strings.TrimSpace(p)
		if a, b, ok := strings.Cut(p, "-"); ok {
			ranges = append(ranges, strings.TrimSpace(a)+":"+strings.TrimSpace(b))
		} else if n, err := strconv.Atoi(p); err == nil {
			ports = append(ports, n)
		}
	}
	if len(ports) > 0 {
		out["port"] = ports
	}
	setIf(out, "port_range", ranges)
	if w.Network == "tcp" || w.Network == "udp" {
		out["network"] = w.Network
	}
	// Протокол узнаётся по первым байтам соединения (действие sniff в начале маршрута).
	setIf(out, "protocol", compact(w.Protocols))

	// Пустое правило (ни «Кто», ни «Что») означает «всё». sing-box так не умеет,
	// поэтому ловим весь трафик через оба сетевых протокола.
	if len(out) == 0 {
		out["network"] = []string{"tcp", "udp"}
	}

	// Куда
	switch rule.Where.Action {
	case "block":
		out["action"] = "reject"
	case "direct":
		out["action"] = "route"
		out["outbound"] = tagDirect
	default:
		out["action"] = "route"
		out["outbound"] = tagProxy
		if t := r.targetTag(rule.Where.Target); t != "" {
			out["outbound"] = t
		}
	}
	return out, true
}

var (
	domainKeys = []string{"domain", "domain_suffix", "domain_keyword", "domain_regex"}
	ipKeys     = []string{"ip_cidr"}
)

// splitByMatch делит правило на часть с доменными условиями и часть с IP.
// Остальные условия (кто, порты, протокол, действие) копируются в обе.
// Если одной из групп условий нет, соответствующая часть — nil;
// если нет ни тех ни других, обе nil.
func splitByMatch(rule M) (domainPart, ipPart M) {
	var sets []string
	if v, ok := rule["rule_set"].([]string); ok {
		sets = v
	}
	var domainSets, ipSets []string
	for _, s := range sets {
		if strings.HasPrefix(s, "geoip-") || strings.HasPrefix(s, "listip-") {
			ipSets = append(ipSets, s)
		} else {
			domainSets = append(domainSets, s)
		}
	}
	hasDomain := len(domainSets) > 0 || hasAny(rule, domainKeys)
	hasIP := len(ipSets) > 0 || hasAny(rule, ipKeys)

	base := M{}
	for k, v := range rule {
		if k != "rule_set" && !slices.Contains(domainKeys, k) && !slices.Contains(ipKeys, k) {
			base[k] = v
		}
	}
	if hasDomain {
		domainPart = maps.Clone(base)
		for _, k := range domainKeys {
			if v, ok := rule[k]; ok {
				domainPart[k] = v
			}
		}
		setIf(domainPart, "rule_set", domainSets)
	}
	if hasIP {
		ipPart = maps.Clone(base)
		for _, k := range ipKeys {
			if v, ok := rule[k]; ok {
				ipPart[k] = v
			}
		}
		setIf(ipPart, "rule_set", ipSets)
	}
	return domainPart, ipPart
}

func hasAny(m M, keys []string) bool {
	for _, k := range keys {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

var ruleSetName = regexp.MustCompile(`^[a-z0-9!@._-]+$`)

func (r *renderer) ruleSet(kind, name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if !ruleSetName.MatchString(name) {
		return ""
	}
	tag := kind + "-" + name
	repo := "sing-geosite"
	if kind == "geoip" {
		repo = "sing-geoip"
	}
	r.ruleSets[tag] = M{
		"type":            "remote",
		"tag":             tag,
		"format":          "binary",
		"url":             fmt.Sprintf("https://raw.githubusercontent.com/SagerNet/%s/rule-set/%s.srs", repo, tag),
		"update_interval": "3d",
	}
	return tag
}

func nodeOutbound(n model.Node) (M, error) {
	ob := M{"tag": nodeTag(n.ID), "server": n.Server, "server_port": n.Port}
	if n.Server == "" || n.Port <= 0 || n.Port > 65535 {
		return nil, fmt.Errorf("не указан адрес или порт")
	}
	switch n.Protocol {
	case "vless":
		ob["type"] = "vless"
		ob["uuid"] = n.UUID
		if n.Flow != "" {
			ob["flow"] = n.Flow
		}
		ob["packet_encoding"] = "xudp"
	case "vmess":
		ob["type"] = "vmess"
		ob["uuid"] = n.UUID
		ob["security"] = orDefault(n.Method, "auto")
		ob["alter_id"] = n.AlterID
		ob["packet_encoding"] = "xudp"
	case "trojan":
		ob["type"] = "trojan"
		ob["password"] = n.Password
	case "shadowsocks":
		ob["type"] = "shadowsocks"
		ob["method"] = n.Method
		ob["password"] = n.Password
	case "hysteria2":
		ob["type"] = "hysteria2"
		ob["password"] = n.Password
		if n.UpMbps > 0 {
			ob["up_mbps"] = n.UpMbps
		}
		if n.DownMbps > 0 {
			ob["down_mbps"] = n.DownMbps
		}
		if n.Obfs != "" {
			ob["obfs"] = M{"type": n.Obfs, "password": n.ObfsPassword}
		}
	case "tuic":
		ob["type"] = "tuic"
		ob["uuid"] = n.UUID
		ob["password"] = n.Password
		ob["congestion_control"] = orDefault(n.Congestion, "bbr")
	default:
		return nil, fmt.Errorf("неизвестный протокол %q", n.Protocol)
	}

	// hysteria2 и tuic работают только поверх TLS.
	security := n.Security
	if n.Protocol == "hysteria2" || n.Protocol == "tuic" {
		if security == "" || security == "none" {
			security = "tls"
		}
	}
	if security == "tls" || security == "reality" {
		tls := M{"enabled": true}
		if sni := orDefault(n.SNI, hostOrEmpty(n.Host)); sni != "" {
			tls["server_name"] = sni
		}
		if n.Insecure {
			tls["insecure"] = true
		}
		if len(n.ALPN) > 0 {
			tls["alpn"] = n.ALPN
		}
		fp := n.Fingerprint
		if security == "reality" {
			fp = orDefault(fp, "chrome") // reality требует uTLS
			tls["reality"] = M{"enabled": true, "public_key": n.PublicKey, "short_id": n.ShortID}
		}
		if fp != "" && fp != "none" {
			tls["utls"] = M{"enabled": true, "fingerprint": fp}
		}
		ob["tls"] = tls
	}

	if t := transport(n); t != nil {
		ob["transport"] = t
	}
	return ob, nil
}

func transport(n model.Node) M {
	switch n.Transport {
	case "ws":
		t := M{"type": "ws"}
		path := orDefault(n.Path, "/")
		// Ранние данные в стиле v2ray: /path?ed=2048
		if u, err := url.Parse(path); err == nil {
			if ed, err := strconv.Atoi(u.Query().Get("ed")); err == nil && ed > 0 {
				t["max_early_data"] = ed
				t["early_data_header_name"] = "Sec-WebSocket-Protocol"
				q := u.Query()
				q.Del("ed")
				u.RawQuery = q.Encode()
				path = u.String()
			}
		}
		t["path"] = path
		if n.Host != "" {
			t["headers"] = M{"Host": n.Host}
		}
		return t
	case "grpc":
		return M{"type": "grpc", "service_name": n.ServiceName}
	case "http", "h2":
		t := M{"type": "http", "path": orDefault(n.Path, "/")}
		if n.Host != "" {
			t["host"] = strings.Split(n.Host, ",")
		}
		return t
	case "httpupgrade":
		t := M{"type": "httpupgrade", "path": orDefault(n.Path, "/")}
		if n.Host != "" {
			t["host"] = n.Host
		}
		return t
	}
	return nil
}

// autoGroup — автоматическая группа. Это обычный переключатель: какой сервер
// в нём выбран, решает NetPilot — проверяет серверы по одному, а не пачкой,
// и переключается только при сбое (app/autoswitch.go). Выбор передаётся
// при каждом запуске ядра, поэтому переживает применение настроек.
func autoGroup(tag string, members []string, selected map[string]string) M {
	g := M{"type": "selector", "tag": tag, "outbounds": members}
	if d := selected[tag]; d != "" && slices.Contains(members, d) {
		g["default"] = d
	}
	return g
}

func groupMembers(cfg *model.Config, g model.Group) ([]string, error) {
	ids, err := core.GroupNodes(cfg, g)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = nodeTag(id)
	}
	return out, nil
}

// dnsServer разбирает адрес DNS: https://1.1.1.1/dns-query, tls://dns.google,
// quic://..., udp://8.8.8.8, 8.8.8.8 или local.
func dnsServer(tag, spec string) (M, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" || spec == "local" {
		return M{"type": "local", "tag": tag}, nil
	}
	if !strings.Contains(spec, "://") {
		spec = "udp://" + spec
	}
	u, err := url.Parse(spec)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("не удалось разобрать %q", spec)
	}
	switch u.Scheme {
	case "udp", "tcp", "tls", "quic", "https", "h3":
	default:
		return nil, fmt.Errorf("неизвестная схема %q", u.Scheme)
	}
	srv := M{"type": u.Scheme, "tag": tag, "server": u.Hostname()}
	if p, err := strconv.Atoi(u.Port()); err == nil {
		srv["server_port"] = p
	}
	if (u.Scheme == "https" || u.Scheme == "h3") && u.Path != "" {
		srv["path"] = u.Path
	}
	if net.ParseIP(u.Hostname()) == nil {
		srv["domain_resolver"] = dnsLocal
	}
	return srv, nil
}

func nodeTagsOf(cfg *model.Config) []string {
	out := make([]string, 0, len(cfg.Nodes))
	for _, n := range cfg.Nodes {
		out = append(out, nodeTag(n.ID))
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

func hostOrEmpty(h string) string {
	h, _, _ = strings.Cut(h, ",")
	return strings.TrimSpace(h)
}

func setIf(m M, key string, v []string) {
	if len(v) > 0 {
		m[key] = v
	}
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

func intersects(a, b []string) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]M) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
