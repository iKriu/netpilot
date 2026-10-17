// Package xray — ядро Xray (отдельный процесс). Умеет XHTTP, которого нет
// в sing-box. Статистику соединений не сообщает (нет Clash API).
package xray

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"netpilot/internal/core"
	"netpilot/internal/core/external"
	"netpilot/internal/model"
)

type Core struct {
	dir   string
	proc  external.Process
	stats statsClient
}

var _ core.Exiter = (*Core)(nil) // за падением процесса следит приложение

func New(dataDir string) *Core { return &Core{dir: filepath.Join(dataDir, "cores", "xray")} }

var binary = external.Exe("xray")

func (c *Core) Name() string            { return "xray" }
func (c *Core) Version() string         { return external.InstalledVersion(c.dir, binary) }
func (c *Core) Available() bool         { return c.Version() != "" }
func (c *Core) Running() bool           { return c.proc.Running() }
func (c *Core) Exited() <-chan struct{} { return c.proc.Exited() }
func (c *Core) ExitReason() string      { return c.proc.ExitReason() }
func (c *Core) ClashAPI() core.Clash    { return core.Clash{} }
func (c *Core) Stop() error {
	c.stats.close()
	return c.proc.Stop()
}

func (c *Core) Capabilities() core.Capabilities {
	return core.Capabilities{
		Protocols:  []string{"vless", "vmess", "trojan", "shadowsocks"},
		Transports: []string{"tcp", "ws", "grpc", "httpupgrade", "xhttp"},
		Security:   []string{"none", "tls", "reality"},
		Detect:     []string{"bittorrent"},
		MemoryMB:   60,
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
		return fmt.Errorf("ядро Xray не установлено")
	}
	data, err := c.Render(cfg, env)
	if err != nil {
		return err
	}
	path := filepath.Join(c.dir, "config.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	bin := filepath.Join(c.dir, binary)
	if err := external.Test(bin, []string{"run", "-test", "-c", path}, c.dir); err != nil {
		return err
	}
	if err := c.proc.Start(bin, []string{"run", "-c", path}, c.dir, filepath.Join(env.DataDir, "core.log")); err != nil {
		return err
	}
	if env.ClashAddr != "" {
		_ = c.stats.open(env.ClashAddr) // адрес API статистики
	}
	return nil
}

func (c *Core) Install(ctx context.Context, dl external.Downloader) (string, error) {
	return external.Install(ctx, dl, release, c.dir)
}

func (c *Core) Uninstall() error { return external.Uninstall(c.dir) }

var release = external.Release{
	Repo: "XTLS/Xray-core",
	Asset: func(string) string {
		name := map[string]string{
			"windows/amd64": "windows-64", "linux/amd64": "linux-64", "linux/arm64": "linux-arm64-v8a",
			"linux/arm": "linux-arm32-v7a", "linux/mipsle": "linux-mips32le", "linux/mips": "linux-mips32",
			"darwin/arm64": "macos-arm64-v8a", "darwin/amd64": "macos-64",
		}[runtime.GOOS+"/"+runtime.GOARCH]
		if name == "" {
			return ""
		}
		return "Xray-" + name + ".zip"
	},
	Digest:  func(asset string) string { return asset + ".dgst" },
	Binary:  binary,
	Extract: []string{"geoip.dat", "geosite.dat"}, // базы стран и категорий сайтов
}

// ---------- конфиг ----------

type M = map[string]any

func render(cfg *model.Config, plan *core.Plan, env core.Env) ([]byte, error) {
	s := cfg.Settings
	sniff := M{"enabled": true, "destOverride": []string{"http", "tls", "quic"}, "routeOnly": true}

	// Вход socks принимает и HTTP-прокси на том же порту.
	inbounds := []M{{
		"tag": "mixed-in", "listen": orDefault(s.ListenAddr, "127.0.0.1"), "port": s.MixedPort,
		"protocol": "socks", "settings": M{"udp": true, "auth": "noauth"}, "sniffing": sniff,
	}}
	if s.Mode == "transparent" && env.OS == "linux" {
		inbounds = append(inbounds, M{
			"tag": "tproxy-in", "listen": "::", "port": s.TProxyPort, "protocol": "dokodemo-door",
			"settings":       M{"network": "tcp,udp", "followRedirect": true},
			"streamSettings": M{"sockopt": M{"tproxy": "tproxy"}},
			"sniffing":       sniff,
		})
	}

	outbounds := []M{
		{"tag": "direct", "protocol": "freedom"},
		{"tag": "block", "protocol": "blackhole"},
	}
	for _, n := range plan.Nodes {
		ob, err := outbound(n)
		if err != nil {
			return nil, fmt.Errorf("сервер «%s»: %w", n.Name, err)
		}
		outbounds = append(outbounds, ob)
	}

	// Группы — балансировщики: «быстрейший» — leastPing по наблюдениям,
	// «вручную» — первый узел (ручного выбора в Xray нет).
	groups := map[string]core.PlanGroup{}
	var balancers []M
	for _, g := range plan.Groups {
		groups[g.Tag] = g
		strategy := "leastPing"
		if !g.Auto {
			strategy = "random"
			g.Members = g.Members[:1]
		}
		balancers = append(balancers, M{"tag": g.Tag, "selector": g.Members, "strategy": M{"type": strategy}})
	}
	// target возвращает поле назначения правила: узел — outboundTag, группа — balancerTag.
	target := func(action, tag string) M {
		switch action {
		case "direct":
			return M{"outboundTag": "direct"}
		case "block":
			return M{"outboundTag": "block"}
		}
		if tag == "" {
			tag = plan.Proxy
		}
		if tag == "" {
			return M{"outboundTag": "direct"}
		}
		if _, ok := groups[tag]; ok {
			return M{"balancerTag": tag}
		}
		return M{"outboundTag": tag}
	}

	var rules []M
	if plan.BypassLAN {
		rules = append(rules, M{"type": "field", "ip": []string{"geoip:private"}, "outboundTag": "direct"})
	}
	for _, r := range plan.Rules {
		// Внешние списки Xray не понимает (об этом предупреждает Validate).
		// Правило только из списков пропускаем, иначе оно стало бы «весь трафик».
		hadLists := len(r.DomainLists)+len(r.IPLists) > 0
		r.DomainLists, r.IPLists = nil, nil
		if hadLists && !r.HasDomain() && !r.HasIP() {
			continue
		}
		// В Xray условия разных полей объединяются через «и», поэтому
		// домены и адреса — отдельными правилами (нам нужно «или»).
		d, ip := r.SplitDomainIP()
		parts := []*core.PlanRule{d, ip}
		if d == nil && ip == nil {
			parts = []*core.PlanRule{&r}
		}
		for _, p := range parts {
			if p == nil {
				continue
			}
			rule := M{"type": "field"}
			for k, v := range target(p.Action, p.Target) {
				rule[k] = v
			}
			if len(p.Sources) > 0 {
				rule["source"] = p.Sources
			}
			var domains []string
			for _, x := range p.Domains {
				domains = append(domains, "full:"+x)
			}
			for _, x := range p.Suffixes {
				domains = append(domains, "domain:"+x)
			}
			for _, x := range p.Keywords {
				domains = append(domains, "keyword:"+x)
			}
			for _, x := range p.Regexps {
				domains = append(domains, "regexp:"+x)
			}
			for _, x := range p.Geosite {
				domains = append(domains, "geosite:"+x)
			}
			if len(domains) > 0 {
				rule["domain"] = domains
			}
			ips := append([]string(nil), p.CIDRs...)
			for _, x := range p.Geoip {
				ips = append(ips, "geoip:"+x)
			}
			if len(ips) > 0 {
				rule["ip"] = ips
			}
			if ports := portList(p); ports != "" {
				rule["port"] = ports
			}
			rule["network"] = orDefault(p.Network, "tcp,udp")
			if len(p.Protocols) > 0 {
				rule["protocol"] = p.Protocols // распознаёт сниффинг входящих
			}
			rules = append(rules, rule)
		}
	}
	// В Xray нет «остального трафика» — это последнее правило, совпадающее со всем.
	final := M{"type": "field", "network": "tcp,udp"}
	for k, v := range target(plan.Final, "") {
		final[k] = v
	}
	rules = append(rules, final)

	conf := M{
		"log":       M{"loglevel": xrayLogLevel(s.LogLevel)},
		"dns":       M{"servers": []any{xrayDNS(s.DNSRemote), xrayDNS(core.DirectDNS(s, env))}},
		"inbounds":  inbounds,
		"outbounds": outbounds,
		"routing": M{
			// Домены проверяются без DNS; разрешение — только если ни одно правило не подошло.
			"domainStrategy": "IPIfNonMatch",
			"rules":          rules,
			"balancers":      balancers,
		},
	}
	// Статистика трафика по выходам (узлам) через gRPC API.
	if env.ClashAddr != "" {
		conf["stats"] = M{}
		conf["api"] = M{"tag": "api", "listen": env.ClashAddr, "services": []string{"StatsService"}}
		conf["policy"] = M{"system": M{"statsOutboundUplink": true, "statsOutboundDownlink": true}}
	}
	if len(balancers) > 0 {
		conf["observatory"] = M{
			"subjectSelector":   []string{"node-"},
			"probeURL":          orDefault(s.TestURL, "https://www.gstatic.com/generate_204"),
			"probeInterval":     "3m",
			"enableConcurrency": true,
		}
	}
	return json.MarshalIndent(conf, "", "  ")
}

func outbound(n core.PlanNode) (M, error) {
	ob := M{"tag": n.Tag}
	switch n.Protocol {
	case "vless":
		user := M{"id": n.UUID, "encryption": "none"}
		if n.Flow != "" {
			user["flow"] = n.Flow
		}
		ob["protocol"] = "vless"
		ob["settings"] = M{"vnext": []M{{"address": n.Server, "port": n.Port, "users": []M{user}}}}
	case "vmess":
		ob["protocol"] = "vmess"
		ob["settings"] = M{"vnext": []M{{"address": n.Server, "port": n.Port, "users": []M{{
			"id": n.UUID, "alterId": n.AlterID, "security": orDefault(n.Method, "auto"),
		}}}}}
	case "trojan":
		ob["protocol"] = "trojan"
		ob["settings"] = M{"servers": []M{{"address": n.Server, "port": n.Port, "password": n.Password}}}
	case "shadowsocks":
		ob["protocol"] = "shadowsocks"
		ob["settings"] = M{"servers": []M{{"address": n.Server, "port": n.Port, "method": n.Method, "password": n.Password}}}
	default:
		return nil, fmt.Errorf("протокол %s не поддерживается ядром Xray", n.Protocol)
	}

	stream := M{"network": network(n.Transport)}
	switch n.Transport {
	case "ws":
		ws := M{"path": orDefault(n.Path, "/")}
		if n.Host != "" {
			ws["host"] = n.Host
		}
		stream["wsSettings"] = ws
	case "grpc":
		stream["grpcSettings"] = M{"serviceName": n.ServiceName}
	case "httpupgrade":
		hu := M{"path": orDefault(n.Path, "/")}
		if n.Host != "" {
			hu["host"] = n.Host
		}
		stream["httpupgradeSettings"] = hu
	case "xhttp":
		xh := M{"path": orDefault(n.Path, "/"), "mode": orDefault(n.XHTTPMode, "auto")}
		if n.Host != "" {
			xh["host"] = n.Host
		}
		stream["xhttpSettings"] = xh
	case "", "tcp":
	default:
		return nil, fmt.Errorf("транспорт %s не поддерживается ядром Xray", n.Transport)
	}
	switch n.Security {
	case "tls":
		tls := M{"serverName": orDefault(n.SNI, n.Host), "fingerprint": orDefault(n.Fingerprint, "chrome")}
		if len(n.ALPN) > 0 {
			tls["alpn"] = n.ALPN
		}
		if n.Insecure {
			tls["allowInsecure"] = true
		}
		stream["security"] = "tls"
		stream["tlsSettings"] = tls
	case "reality":
		stream["security"] = "reality"
		stream["realitySettings"] = M{
			"serverName": n.SNI, "fingerprint": orDefault(n.Fingerprint, "chrome"),
			"publicKey": n.PublicKey, "shortId": n.ShortID,
		}
	}
	ob["streamSettings"] = stream
	return ob, nil
}

func network(t string) string {
	if t == "" || t == "tcp" {
		return "raw"
	}
	return t
}

func portList(r *core.PlanRule) string {
	var parts []string
	for _, p := range r.Ports {
		parts = append(parts, strconv.Itoa(p))
	}
	for _, pr := range r.PortRanges {
		parts = append(parts, fmt.Sprintf("%d-%d", pr[0], pr[1]))
	}
	return strings.Join(parts, ",")
}

// xrayDNS: Xray понимает https://, tcp://, quic+local:// и просто IP.
func xrayDNS(spec string) any {
	spec = strings.TrimSpace(spec)
	if spec == "" || spec == "local" {
		return "localhost"
	}
	if u, err := url.Parse(spec); err == nil && u.Scheme == "tls" {
		// DNS over TLS в Xray нет — используем DoH того же сервера.
		return "https://" + u.Host + "/dns-query"
	}
	if net.ParseIP(spec) != nil {
		return spec
	}
	return spec
}

func xrayLogLevel(l string) string {
	switch l {
	case "debug", "info", "error":
		return l
	case "warn":
		return "warning"
	}
	return "warning"
}

func orDefault(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
