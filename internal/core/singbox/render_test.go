package singbox

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/option"
	sjson "github.com/sagernet/sing/common/json"

	"netpilot/internal/core"
	"netpilot/internal/links"
	"netpilot/internal/model"
)

// Проверяем, что sing-box принимает сгенерированный конфиг со всеми
// протоколами, группами и типами правил.
func TestRenderAcceptedBySingBox(t *testing.T) {
	nodes, errs := links.ParseMany(`
vless://b831381d-6324-4d53-ad4f-8cda48b30811@1.2.3.4:443?type=tcp&security=reality&sni=www.microsoft.com&fp=chrome&pbk=jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0&sid=ab12&flow=xtls-rprx-vision#DE-1
vless://b831381d-6324-4d53-ad4f-8cda48b30811@de2.example.com:443?type=ws&security=tls&path=%2Fws%3Fed%3D2048&host=cdn.example.com#DE-2
trojan://secret@tr.example.com:8443?type=grpc&serviceName=gun&sni=tr.example.com#NL-1
ss://YWVzLTI1Ni1nY206cGFzcw@5.6.7.8:8388#SS
hy2://pw@hy.example.com:443?sni=hy.example.com&obfs=salamander&obfs-password=x#HY
tuic://b831381d-6324-4d53-ad4f-8cda48b30811:pw@tu.example.com:443?congestion_control=bbr&alpn=h3#TU
`)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	cfg := model.Defaults()
	for i := range nodes {
		nodes[i].ID = string(rune('a' + i))
	}
	cfg.Nodes = nodes
	cfg.Groups = []model.Group{
		{ID: "de", Name: "Германия", Type: "auto", Filter: "^DE", Exclude: "LTE"},
		{ID: "manual", Name: "Ручной", Type: "select", NodeIDs: []string{"c", "d"}},
		{ID: "empty", Name: "Пустая", Type: "auto", Filter: "^XX"},
	}
	cfg.Devices = []model.Device{
		{ID: "tv", Name: "ТВ", IP: "192.168.1.50", Tags: []string{"media"}, Route: "proxy", RouteTarget: "manual"},
		{ID: "kid", Name: "Планшет", IP: "192.168.1.60", Route: "block"},
		{ID: "pc", Name: "ПК", IP: "192.168.1.61", Route: "block"},
		{ID: "noip", Name: "Без IP"},
	}
	cfg.Rules = []model.Rule{
		{ID: "1", Enabled: true, What: model.What{Apps: []string{"youtube", "telegram"}}, Where: model.Where{Action: "proxy", Target: "de"}},
		{ID: "2", Enabled: true, Who: model.Who{Tags: []string{"media"}}, Where: model.Where{Action: "direct"}},
		{ID: "3", Enabled: true, What: model.What{Domains: []string{"full:a.example.com", "keyword:ads", "regexp:^x\\.", "*.ru"}}, Where: model.Where{Action: "block"}},
		{ID: "4", Enabled: true, What: model.What{Geosite: []string{"category-ru"}, Geoip: []string{"ru"}}, Where: model.Where{Action: "direct"}},
		{ID: "5", Enabled: true, What: model.What{Ports: []string{"27000-27100", "3478"}, Network: "udp"}, Where: model.Where{Action: "direct"}},
		{ID: "6", Enabled: true, Who: model.Who{DeviceIDs: []string{"noip"}}, Where: model.Where{Action: "block"}},
		{ID: "8", Enabled: true, What: model.What{Protocols: []string{"bittorrent"}}, Where: model.Where{Action: "direct"}},
		{ID: "7", Enabled: true, Where: model.Where{Action: "proxy", Target: "manual"}},
	}
	cfg.Settings.DefaultTarget = "de"
	cfg.Settings.Mode = "transparent"
	cfg.Settings.DNSRemote = "tls://dns.google"

	data, err := Render(cfg, core.Env{DataDir: t.TempDir(), OS: "linux", ClashAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(include.Context(context.Background()))
	defer cancel()
	opts, err := sjson.UnmarshalExtendedContext[option.Options](ctx, data)
	if err != nil {
		t.Fatalf("sing-box не разобрал конфиг: %v\n%s", err, data)
	}
	instance, err := box.New(box.Options{Context: ctx, Options: opts})
	if err != nil {
		t.Fatalf("sing-box отверг конфиг: %v\n%s", err, data)
	}
	_ = instance.Close()

	var parsed struct {
		Route struct {
			Rules []map[string]any `json:"rules"`
		} `json:"route"`
		DNS struct {
			Rules []map[string]any `json:"rules"`
		} `json:"dns"`
		Inbounds []map[string]any `json:"inbounds"`
	}
	_ = json.Unmarshal(data, &parsed)
	// sniff, hijack-dns, bypass LAN (3)
	// + маршруты устройств: ТВ через группу, планшет и ПК заблокированы одним правилом (2)
	// + правило 1: домены YouTube/Telegram, resolve, IP Telegram (3)
	// + правила 2, 3, 5, 8 (торренты), 7 (5) + правило 4: geosite, geoip (2)
	// Правило 6 пропущено: у устройства нет IP.
	rules := parsed.Route.Rules
	if got := len(rules); got != 15 {
		t.Fatalf("правил в маршруте: %d, ожидалось 15\n%s", got, data)
	}
	if rules[3]["outbound"] != "group-manual" || rules[4]["action"] != "reject" {
		t.Errorf("маршруты устройств: %v / %v", rules[3], rules[4])
	}
	if got := rules[4]["source_ip_cidr"].([]any); len(got) != 2 {
		t.Errorf("заблокированные устройства должны быть в одном правиле: %v", got)
	}
	if len(parsed.Inbounds) != 2 || parsed.Inbounds[1]["type"] != "tproxy" {
		t.Errorf("в прозрачном режиме нужен вход tproxy: %v", parsed.Inbounds)
	}
	// Прямые доменные правила (3 «блок» не прямое; 4 — geosite; 2 и 5 без доменов).
	if n := len(parsed.DNS.Rules); n != 1 {
		t.Errorf("DNS-правил %d, ожидалось 1: %v", n, parsed.DNS.Rules)
	} else if rs := parsed.DNS.Rules[0]["rule_set"].([]any); len(rs) != 1 || rs[0] != "geosite-category-ru" {
		t.Errorf("DNS-правило должно содержать только geosite: %v", parsed.DNS.Rules[0])
	}
	// Доменная часть проверяется до DNS-запроса.
	if _, ok := rules[5]["domain_suffix"]; !ok {
		t.Errorf("rules[5] должно быть доменной частью: %v", rules[5])
	}
	if rules[6]["action"] != "resolve" {
		t.Errorf("rules[6] должно быть resolve: %v", rules[6])
	}
	if _, ok := rules[7]["ip_cidr"]; !ok {
		t.Errorf("rules[7] должно быть IP-частью: %v", rules[7])
	}
}

func TestSplitByMatch(t *testing.T) {
	d, ip := splitByMatch(M{
		"source_ip_cidr": []string{"10.0.0.2/32"},
		"domain_suffix":  []string{"ru"},
		"rule_set":       []string{"geosite-category-ru", "geoip-ru"},
		"action":         "route", "outbound": "direct",
	})
	if d == nil || ip == nil {
		t.Fatal("ожидались обе части")
	}
	if _, ok := d["source_ip_cidr"]; !ok {
		t.Error("условие «кто» потеряно в доменной части")
	}
	if got := d["rule_set"].([]string); len(got) != 1 || got[0] != "geosite-category-ru" {
		t.Errorf("доменная часть: rule_set=%v", got)
	}
	if got := ip["rule_set"].([]string); len(got) != 1 || got[0] != "geoip-ru" {
		t.Errorf("IP-часть: rule_set=%v", got)
	}
	if _, ok := ip["domain_suffix"]; ok {
		t.Error("домены попали в IP-часть")
	}

	d, ip = splitByMatch(M{"network": []string{"tcp", "udp"}, "action": "reject"})
	if d != nil || ip != nil {
		t.Error("правило без доменов и IP не должно делиться")
	}
}

func TestRenderLists(t *testing.T) {
	cfg := model.Defaults()
	cfg.Rules = []model.Rule{{ID: "r", Enabled: true, What: model.What{Lists: []string{"refilter-domains", "refilter-ips"}}, Where: model.Where{Action: "proxy"}}}
	dir := t.TempDir()
	rules := func() []map[string]any {
		data, err := Render(cfg, core.Env{DataDir: dir, OS: "linux", ListsDir: dir})
		if err != nil {
			t.Fatal(err)
		}
		var p struct {
			Route struct {
				Rules []map[string]any `json:"rules"`
			} `json:"route"`
		}
		_ = json.Unmarshal(data, &p)
		return p.Route.Rules
	}
	// Списки не скачаны — правило пропущено, а не «весь трафик».
	for _, r := range rules() {
		if r["outbound"] == "proxy" {
			t.Fatalf("правило без скачанных списков попало в конфиг: %v", r)
		}
	}
	for _, f := range []string{"refilter-domains.srs", "refilter-ips.srs"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("SRS\x01"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var domainRule, ipRule bool
	for _, r := range rules() {
		sets, _ := r["rule_set"].([]any)
		for _, s := range sets {
			domainRule = domainRule || s == "listd-refilter-domains"
			ipRule = ipRule || s == "listip-refilter-ips"
		}
	}
	if !domainRule || !ipRule {
		t.Errorf("ожидались правила с доменным и IP-списком: %v", rules())
	}
}

// IPv6-адреса устройства входят в его маршрут и правила по устройству, а
// Clash API защищён ключом.
func TestRenderDeviceIPv6AndSecret(t *testing.T) {
	cfg := model.Defaults()
	cfg.Nodes = []model.Node{{ID: "a", Name: "A", Protocol: "shadowsocks", Server: "1.2.3.4", Port: 8388, Method: "aes-256-gcm", Password: "x"}}
	cfg.Devices = []model.Device{{ID: "tv", IP: "192.168.1.50", Route: "block"}}
	cfg.Rules = []model.Rule{{ID: "1", Enabled: true, Who: model.Who{DeviceIDs: []string{"tv"}}, Where: model.Where{Action: "direct"}}}
	env := core.Env{
		DataDir: t.TempDir(), OS: "linux", ClashAddr: "127.0.0.1:0", ClashSecret: "s3",
		DeviceIPv6: map[string][]string{"tv": {"2001:db8::50", "fe80::1"}},
	}
	data, err := Render(cfg, env)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Route struct {
			Rules []map[string]any `json:"rules"`
		} `json:"route"`
		Experimental struct {
			ClashAPI map[string]any `json:"clash_api"`
		} `json:"experimental"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Experimental.ClashAPI["secret"] != "s3" {
		t.Errorf("clash_api без ключа: %v", parsed.Experimental.ClashAPI)
	}
	var withSrc []string
	for _, r := range parsed.Route.Rules {
		if src, ok := r["source_ip_cidr"].([]any); ok {
			withSrc = append(withSrc, fmt.Sprint(src))
		}
	}
	want := "[192.168.1.50/32 2001:db8::50/128]"
	if len(withSrc) != 2 || withSrc[0] != want || withSrc[1] != want {
		t.Fatalf("источники маршрута и правила: %v, ожидалось %s дважды\n%s", withSrc, want, data)
	}
}

// IP-списки (Re:filter) ведут через прокси только веб-трафик: иначе пиры
// торрентов из подсетей хостингов уходили через прокси, а зашифрованное
// рукопожатие BitTorrent правило «торренты напрямую» не распознаёт.
func TestListsOnlyForWeb(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "refilter-ips.srs"), []byte("SRS\x01"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := model.Defaults()
	cfg.Rules = []model.Rule{
		{ID: "t", Enabled: true, What: model.What{Protocols: []string{"bittorrent"}}, Where: model.Where{Action: "direct"}},
		{ID: "l", Enabled: true, What: model.What{Lists: []string{"refilter-ips"}, IPs: []string{"203.0.113.0/24"}}, Where: model.Where{Action: "proxy"}},
		{ID: "p", Enabled: true, What: model.What{Lists: []string{"refilter-ips"}, Ports: []string{"6881"}}, Where: model.Where{Action: "block"}},
	}
	data, err := Render(cfg, core.Env{DataDir: dir, OS: "linux", ListsDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Route struct {
			Rules []map[string]any `json:"rules"`
		} `json:"route"`
	}
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	var byProto, byPort, cidr, userPort bool
	for _, r := range p.Route.Rules {
		sets, _ := r["rule_set"].([]any)
		hasList := len(sets) == 1 && sets[0] == "listip-refilter-ips"
		_, proto := r["protocol"]
		_, port := r["port"]
		switch {
		case hasList && r["action"] == "reject":
			userPort = port && !proto // явный порт человека — без веб-ограничения
		case hasList && proto && !port:
			byProto = true
		case hasList && port && !proto:
			byPort = true
		case hasList:
			t.Errorf("IP-список без ограничения веб-трафиком: %v", r)
		case r["ip_cidr"] != nil && r["outbound"] == "proxy":
			cidr = !proto && !port // явные адреса — как раньше, без ограничения
		}
	}
	if !byProto || !byPort || !cidr || !userPort {
		t.Errorf("протокол=%v порты=%v cidr=%v порт-человека=%v\n%s", byProto, byPort, cidr, userPort, data)
	}
	// sing-box должен принимать такие поля в правилах.
	ctx, cancel := context.WithCancel(include.Context(context.Background()))
	defer cancel()
	if _, err := sjson.UnmarshalExtendedContext[option.Options](ctx, data); err != nil {
		t.Fatalf("sing-box не разобрал конфиг: %v", err)
	}
}
