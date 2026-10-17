// Package mihomo — ядро mihomo (Clash.Meta, отдельный процесс). Даёт Clash API:
// задержки, соединения, трафик — как у sing-box.
package mihomo

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"netpilot/internal/core"
	"netpilot/internal/core/external"
	"netpilot/internal/model"
)

type Core struct {
	dir   string
	proc  external.Process
	mu    sync.Mutex
	clash string
}

var (
	_ core.Exiter  = (*Core)(nil) // за падением процесса следит приложение
	_ core.Checker = (*Core)(nil)
)

func New(dataDir string) *Core { return &Core{dir: filepath.Join(dataDir, "cores", "mihomo")} }

var binary = external.Exe("mihomo")

func (c *Core) Name() string            { return "mihomo" }
func (c *Core) Version() string         { return external.InstalledVersion(c.dir, binary) }
func (c *Core) Available() bool         { return c.Version() != "" }
func (c *Core) Running() bool           { return c.proc.Running() }
func (c *Core) Exited() <-chan struct{} { return c.proc.Exited() }
func (c *Core) ExitReason() string      { return c.proc.ExitReason() }

func (c *Core) ClashAPI() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.clash
}

func (c *Core) Stop() error {
	c.mu.Lock()
	c.clash = ""
	c.mu.Unlock()
	return c.proc.Stop()
}

func (c *Core) Capabilities() core.Capabilities {
	return core.Capabilities{
		Protocols:  []string{"vless", "vmess", "trojan", "shadowsocks", "hysteria2", "tuic"},
		Transports: []string{"tcp", "ws", "grpc", "http", "h2", "httpupgrade"},
		Security:   []string{"none", "tls", "reality"},
		MemoryMB:   50,
		Lists:      true,
	}
}

func (c *Core) Render(cfg *model.Config, env core.Env) ([]byte, error) {
	plan, err := core.BuildPlan(cfg, env)
	if err != nil {
		return nil, err
	}
	return render(cfg, plan, env)
}

func (c *Core) Start(cfg *model.Config, env core.Env) error {
	if !c.Available() {
		return fmt.Errorf("ядро mihomo не установлено")
	}
	data, err := c.Render(cfg, env)
	if err != nil {
		return err
	}
	path := filepath.Join(c.dir, "config.yaml") // JSON — корректный YAML
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	bin := filepath.Join(c.dir, binary)
	if err := external.Test(bin, []string{"-d", c.dir, "-f", path, "-t"}, c.dir); err != nil {
		return err
	}
	if err := c.proc.Start(bin, []string{"-d", c.dir, "-f", path}, c.dir, filepath.Join(env.DataDir, "core.log")); err != nil {
		return err
	}
	c.mu.Lock()
	c.clash = env.ClashAddr
	c.mu.Unlock()
	return nil
}

// Reload применяет новую конфигурацию через API mihomo, не перезапуская процесс.
// Сначала конфиг проверяется самим mihomo: при ошибке продолжает работать прежний.
func (c *Core) Reload(cfg *model.Config, env core.Env) error {
	c.mu.Lock()
	api := c.clash
	c.mu.Unlock()
	if api == "" || !c.proc.Running() {
		return core.ErrReloadUnsupported
	}
	env.ClashAddr = api // адрес API не меняем, иначе потеряем связь с ядром
	data, err := c.Render(cfg, env)
	if err != nil {
		return err
	}
	next := filepath.Join(c.dir, "config.next.yaml")
	if err := os.WriteFile(next, data, 0o600); err != nil {
		return err
	}
	if err := external.Test(filepath.Join(c.dir, binary), []string{"-d", c.dir, "-f", next, "-t"}, c.dir); err != nil {
		return err
	}
	path := filepath.Join(c.dir, "config.yaml")
	if err := os.Rename(next, path); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{"path": path})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPut, "http://"+api+"/configs?force=true", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("mihomo не принял конфигурацию: %s", strings.TrimSpace(string(msg)))
	}
	return nil
}

func (c *Core) Install(ctx context.Context, dl external.Downloader) (string, error) {
	return external.Install(ctx, dl, release, c.dir)
}

func (c *Core) Uninstall() error { return external.Uninstall(c.dir) }

var release = external.Release{
	Repo: "MetaCubeX/mihomo",
	Asset: func(tag string) string {
		// compatible — без требований к новым инструкциям процессора.
		name := map[string]string{
			"windows/amd64": "windows-amd64-compatible", "linux/amd64": "linux-amd64-compatible",
			"linux/arm64": "linux-arm64", "linux/arm": "linux-armv7", "linux/mipsle": "linux-mipsle-softfloat",
			"linux/mips": "linux-mips-softfloat", "darwin/arm64": "darwin-arm64", "darwin/amd64": "darwin-amd64-compatible",
		}[runtime.GOOS+"/"+runtime.GOARCH]
		if name == "" {
			return ""
		}
		ext := ".gz"
		if runtime.GOOS == "windows" {
			ext = ".zip"
		}
		return "mihomo-" + name + "-" + tag + ext
	},
	Binary:    binary,
	InArchive: func(base string) bool { return strings.HasPrefix(base, "mihomo") && strings.HasSuffix(base, ".exe") },
}

// ---------- конфиг ----------

type M = map[string]any

func render(cfg *model.Config, plan *core.Plan, env core.Env) ([]byte, error) {
	s := cfg.Settings
	listen := orDefault(s.ListenAddr, "127.0.0.1")
	conf := M{
		"mixed-port":        s.MixedPort,
		"allow-lan":         listen != "127.0.0.1" && listen != "::1",
		"bind-address":      bindAddress(listen),
		"mode":              "rule",
		"log-level":         logLevel(s.LogLevel),
		"ipv6":              true,
		"find-process-mode": "off",
		"geodata-mode":      true, // GeoSite/GeoIP в формате .dat, как у остальных ядер
		"geo-auto-update":   false,
		"profile":           M{"store-selected": true},
	}
	if env.ClashAddr != "" {
		conf["external-controller"] = env.ClashAddr
	}
	if s.Mode == "transparent" && env.OS == "linux" {
		conf["tproxy-port"] = s.TProxyPort
		// TPROXY-вход mihomo слушает тот же адрес, что и прокси. Перехваченные
		// пакеты адресованы чужим IP, поэтому нужен адрес «все».
		conf["allow-lan"], conf["bind-address"] = true, "*"
	}
	conf["dns"] = M{
		"enable":             true,
		"ipv6":               true,
		"default-nameserver": []string{bootstrapIP(s.DNSDirect)},
		"nameserver":         []string{dnsSpec(s.DNSDirect)},
		// Адреса серверов прокси — прямым DNS, иначе запрос уйдёт в ещё не поднятый прокси.
		"proxy-server-nameserver": []string{dnsSpec(s.DNSDirect)},
	}

	var proxies []M
	for _, n := range plan.Nodes {
		p, err := proxy(n)
		if err != nil {
			return nil, fmt.Errorf("сервер «%s»: %w", n.Name, err)
		}
		proxies = append(proxies, p)
	}
	testURL := orDefault(s.TestURL, "https://www.gstatic.com/generate_204")
	var groups []M
	var selectable []string
	for _, g := range plan.Groups {
		gr := M{"name": g.Tag, "proxies": g.Members}
		if g.Auto {
			// Как в sing-box: держимся за выбранный, переключаемся при отказе.
			gr["type"], gr["url"], gr["interval"], gr["tolerance"] = "url-test", testURL, core.AutoIntervalSec, core.AutoTolerance
		} else {
			gr["type"] = "select"
		}
		groups = append(groups, gr)
		selectable = append(selectable, g.Tag)
	}
	for _, n := range plan.Nodes {
		selectable = append(selectable, n.Tag)
	}
	// «proxy» — выбор по умолчанию; первым идёт заданный в настройках.
	proxyGroup := []string{"DIRECT"}
	if plan.Proxy != "" {
		proxyGroup = []string{plan.Proxy}
		for _, t := range selectable {
			if t != plan.Proxy {
				proxyGroup = append(proxyGroup, t)
			}
		}
	}
	groups = append(groups, M{"name": "proxy", "type": "select", "proxies": proxyGroup})

	target := func(action, tag string) string {
		switch action {
		case "direct":
			return "DIRECT"
		case "block":
			return "REJECT"
		}
		if tag == "" {
			return "proxy"
		}
		return tag
	}
	var rules []string
	if plan.BypassLAN {
		for _, c := range core.PrivateCIDRs {
			kind := "IP-CIDR"
			if strings.Contains(c, ":") {
				kind = "IP-CIDR6"
			}
			rules = append(rules, kind+","+c+",DIRECT,no-resolve")
		}
	}
	for _, r := range plan.Rules {
		// Распознавания протоколов (BitTorrent) в mihomo нет. Такое правило
		// пропускаем: без этого условия оно сработало бы для всего трафика.
		if len(r.Protocols) > 0 {
			continue
		}
		d, ip := r.SplitDomainIP()
		parts := []*core.PlanRule{d, ip}
		if d == nil && ip == nil {
			parts = []*core.PlanRule{&r}
		}
		for _, p := range parts {
			if p != nil {
				if line := rule(p, target(p.Action, p.Target)); line != "" {
					rules = append(rules, line)
				}
			}
		}
	}
	rules = append(rules, "MATCH,"+target(plan.Final, ""))

	conf["proxies"] = proxies
	conf["proxy-groups"] = groups
	conf["rules"] = rules
	// Скачанные списки (Re:filter) — наборы правил из локальных файлов.
	providers := M{}
	for _, r := range plan.Rules {
		for id, path := range r.DomainLists {
			providers[id] = M{"type": "file", "behavior": "domain", "format": "text", "path": path}
		}
		for id, path := range r.IPLists {
			providers[id] = M{"type": "file", "behavior": "ipcidr", "format": "text", "path": path}
		}
	}
	if len(providers) > 0 {
		conf["rule-providers"] = providers
	}
	return json.MarshalIndent(conf, "", "  ")
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// unsafe — значение, которое сломает строку правила mihomo: запятая и скобки
// там разделители, а экранирования нет. С таким правилом mihomo не запустится.
func unsafe(v string) bool { return strings.ContainsAny(v, ",()") }

// Check — условия правил, которые mihomo не может записать (см. unsafe).
// Они пропускаются при сборке конфига; здесь — объяснение для интерфейса.
func (c *Core) Check(cfg *model.Config) []core.Issue {
	var issues []core.Issue
	for _, r := range cfg.Rules {
		if !r.Enabled {
			continue
		}
		var bad []string
		for _, list := range [][]string{r.What.Domains, r.What.Geosite, r.What.Geoip} {
			for _, v := range list {
				if unsafe(v) {
					bad = append(bad, "«"+v+"»")
				}
			}
		}
		if len(bad) > 0 {
			issues = append(issues, core.Issue{Message: fmt.Sprintf(
				"правило «%s»: %s — запятые и скобки mihomo не понимает, условие пропущено", r.Name, strings.Join(bad, ", "))})
		}
	}
	return issues
}

// rule собирает правило mihomo: внутри блока — OR, между блоками — AND.
// Значения с запятой или скобками пропускаются (см. unsafe); если от условий
// на домены не осталось ничего, правило не создаётся ("") — иначе оно
// сработало бы для всего трафика.
func rule(r *core.PlanRule, target string) string {
	var blocks []string
	add := func(items []string) {
		switch len(items) {
		case 0:
		case 1:
			blocks = append(blocks, items[0])
		default:
			blocks = append(blocks, "OR,("+wrap(items)+")")
		}
	}
	var src []string
	for _, c := range r.Sources {
		src = append(src, "SRC-IP-CIDR,"+c)
	}
	add(src)

	var what []string
	skipped := false
	addSafe := func(kind string, items []string) {
		for _, x := range items {
			if unsafe(x) {
				skipped = true
				continue
			}
			what = append(what, kind+","+x)
		}
	}
	addSafe("DOMAIN", r.Domains)
	addSafe("DOMAIN-SUFFIX", r.Suffixes)
	addSafe("DOMAIN-KEYWORD", r.Keywords)
	addSafe("DOMAIN-REGEX", r.Regexps)
	addSafe("GEOSITE", r.Geosite)
	for _, x := range r.Geoip {
		if unsafe(x) {
			skipped = true
			continue
		}
		what = append(what, "GEOIP,"+strings.ToUpper(x))
	}
	for _, x := range r.CIDRs {
		if strings.Contains(x, ":") {
			what = append(what, "IP-CIDR6,"+x)
		} else {
			what = append(what, "IP-CIDR,"+x)
		}
	}
	for _, id := range sortedKeys(r.DomainLists) {
		what = append(what, "RULE-SET,"+id)
	}
	for _, id := range sortedKeys(r.IPLists) {
		what = append(what, "RULE-SET,"+id)
	}
	if skipped && len(what) == 0 {
		return ""
	}
	add(what)

	var ports []string
	for _, p := range r.Ports {
		ports = append(ports, fmt.Sprintf("DST-PORT,%d", p))
	}
	for _, pr := range r.PortRanges {
		ports = append(ports, fmt.Sprintf("DST-PORT,%d-%d", pr[0], pr[1]))
	}
	add(ports)
	if r.Network != "" {
		blocks = append(blocks, "NETWORK,"+strings.ToUpper(r.Network))
	}

	switch len(blocks) {
	case 0:
		return "MATCH," + target
	case 1:
		return blocks[0] + "," + target
	default:
		return "AND,(" + wrap(blocks) + ")," + target
	}
}

func wrap(items []string) string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = "(" + it + ")"
	}
	return strings.Join(out, ",")
}

func proxy(n core.PlanNode) (M, error) {
	p := M{"name": n.Tag, "server": n.Server, "port": n.Port, "udp": true}
	switch n.Protocol {
	case "vless":
		p["type"], p["uuid"] = "vless", n.UUID
		if n.Flow != "" {
			p["flow"] = n.Flow
		}
		p["packet-encoding"] = "xudp"
	case "vmess":
		p["type"], p["uuid"], p["alterId"], p["cipher"] = "vmess", n.UUID, n.AlterID, orDefault(n.Method, "auto")
	case "trojan":
		p["type"], p["password"] = "trojan", n.Password
	case "shadowsocks":
		p["type"], p["cipher"], p["password"] = "ss", n.Method, n.Password
	case "hysteria2":
		p["type"], p["password"] = "hysteria2", n.Password
		if n.Obfs != "" {
			p["obfs"], p["obfs-password"] = n.Obfs, n.ObfsPassword
		}
		if n.UpMbps > 0 {
			p["up"] = fmt.Sprintf("%d Mbps", n.UpMbps)
		}
		if n.DownMbps > 0 {
			p["down"] = fmt.Sprintf("%d Mbps", n.DownMbps)
		}
	case "tuic":
		p["type"], p["uuid"], p["password"] = "tuic", n.UUID, n.Password
		p["congestion-controller"] = orDefault(n.Congestion, "bbr")
		p["udp-relay-mode"] = "native"
	default:
		return nil, fmt.Errorf("протокол %s не поддерживается ядром mihomo", n.Protocol)
	}

	sec := n.Security
	if n.Protocol == "hysteria2" || n.Protocol == "tuic" || n.Protocol == "trojan" {
		sec = orDefault(sec, "tls")
		if sec == "none" {
			sec = "tls"
		}
	}
	if sec == "tls" || sec == "reality" {
		sni := orDefault(n.SNI, n.Host)
		switch n.Protocol {
		case "vless", "vmess":
			p["tls"] = true
			if sni != "" {
				p["servername"] = sni
			}
		default:
			if sni != "" {
				p["sni"] = sni
			}
		}
		if n.Insecure {
			p["skip-cert-verify"] = true
		}
		if len(n.ALPN) > 0 {
			p["alpn"] = n.ALPN
		}
		if n.Fingerprint != "" || sec == "reality" {
			p["client-fingerprint"] = orDefault(n.Fingerprint, "chrome")
		}
		if sec == "reality" {
			p["reality-opts"] = M{"public-key": n.PublicKey, "short-id": n.ShortID}
		}
	}

	switch n.Transport {
	case "", "tcp":
	case "ws", "httpupgrade":
		opts := M{"path": orDefault(n.Path, "/")}
		if n.Host != "" {
			opts["headers"] = M{"Host": n.Host}
		}
		if n.Transport == "httpupgrade" {
			opts["v2ray-http-upgrade"] = true
		}
		p["network"], p["ws-opts"] = "ws", opts
	case "grpc":
		p["network"], p["grpc-opts"] = "grpc", M{"grpc-service-name": n.ServiceName}
	case "http", "h2":
		opts := M{"path": orDefault(n.Path, "/")}
		if n.Host != "" {
			opts["host"] = strings.Split(n.Host, ",")
		}
		p["network"], p["h2-opts"] = "h2", opts
	default:
		return nil, fmt.Errorf("транспорт %s не поддерживается ядром mihomo", n.Transport)
	}
	return p, nil
}

func bindAddress(listen string) string {
	if listen == "0.0.0.0" || listen == "::" {
		return "*"
	}
	return listen
}

// dnsSpec: mihomo понимает https://, tls://, quic://, udp:// и IP.
func dnsSpec(spec string) string {
	spec = strings.TrimSpace(spec)
	if spec == "" || spec == "local" {
		return "system"
	}
	return spec
}

// bootstrapIP — IP для разрешения адресов самих DNS-серверов.
func bootstrapIP(spec string) string {
	if u, err := url.Parse(spec); err == nil && net.ParseIP(u.Hostname()) != nil {
		return u.Hostname()
	}
	if net.ParseIP(spec) != nil {
		return spec
	}
	return "77.88.8.8"
}

func logLevel(l string) string {
	switch l {
	case "debug", "info", "error":
		return l
	case "warn":
		return "warning"
	}
	return "info"
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
