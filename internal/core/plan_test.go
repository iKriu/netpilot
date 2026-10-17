package core

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"netpilot/internal/model"
)

// IPv6-адреса устройства попадают в его маршрут и в правила по устройству
// и тегу; локальные и неверные адреса пропускаются.
func TestPlanDeviceIPv6(t *testing.T) {
	cfg := model.Defaults()
	cfg.Nodes = []model.Node{{ID: "a", Server: "1.2.3.4", Port: 443}}
	cfg.Devices = []model.Device{
		{ID: "tv", IP: "192.168.1.50", Route: "proxy", Tags: []string{"media"}},
		{ID: "v6only", Route: "block"},
		{ID: "none", Route: "direct"},
	}
	cfg.Rules = []model.Rule{
		{ID: "1", Enabled: true, Who: model.Who{Tags: []string{"media"}}, Where: model.Where{Action: "direct"}},
		{ID: "2", Enabled: true, Who: model.Who{DeviceIDs: []string{"v6only"}}, Where: model.Where{Action: "block"}},
		{ID: "3", Enabled: true, Who: model.Who{DeviceIDs: []string{"none"}}, Where: model.Where{Action: "block"}},
	}
	env := Env{DeviceIPv6: map[string][]string{
		"tv":     {"2001:db8::50", "fe80::1", "мусор", "fd00::50%eth0", "::ffff:1.2.3.4"},
		"v6only": {"2001:db8::60"},
	}}
	p, err := BuildPlan(cfg, env)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"192.168.1.50/32", "2001:db8::50/128"}, // маршрут ТВ
		{"2001:db8::60/128"},                    // маршрут устройства только с IPv6
		{"192.168.1.50/32", "2001:db8::50/128"}, // правило по тегу
		{"2001:db8::60/128"},                    // правило по устройству
	}
	if len(p.Rules) != len(want) {
		t.Fatalf("правил %d, ожидалось %d: %+v", len(p.Rules), len(want), p.Rules)
	}
	for i, w := range want {
		if !slices.Equal(p.Rules[i].Sources, w) {
			t.Errorf("правило %d: источники %v, ожидались %v", i, p.Rules[i].Sources, w)
		}
	}
}

// Каждый запрос к Clash API несёт ключ.
func TestClashRequest(t *testing.T) {
	req, err := Clash{Addr: "127.0.0.1:9090", Secret: "s3"}.Request(context.Background(), http.MethodGet, "/connections", nil)
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.String() != "http://127.0.0.1:9090/connections" || req.Header.Get("Authorization") != "Bearer s3" {
		t.Fatalf("запрос: %s, заголовок %q", req.URL, req.Header.Get("Authorization"))
	}
}

// Для mihomo IP-списки получают веб-порты, остальные IP-условия — без изменений.
func TestSplitIPLists(t *testing.T) {
	r := PlanRule{CIDRs: []string{"203.0.113.0/24"}, IPLists: map[string]string{"refilter-ips": "x"}, Action: "proxy"}
	rest, lists := r.SplitIPLists()
	if rest == nil || len(rest.IPLists) != 0 || len(rest.CIDRs) != 1 || len(rest.Ports) != 0 {
		t.Fatalf("rest: %+v", rest)
	}
	if lists == nil || len(lists.CIDRs) != 0 || len(lists.IPLists) != 1 || len(lists.Ports) != len(WebPorts) {
		t.Fatalf("lists: %+v", lists)
	}
	withPorts := PlanRule{IPLists: map[string]string{"refilter-ips": "x"}, Ports: []int{6881}}
	if rest, lists := withPorts.SplitIPLists(); lists != nil || rest == nil || len(rest.Ports) != 1 {
		t.Fatalf("явные порты должны остаться как есть: %+v %+v", rest, lists)
	}
}
