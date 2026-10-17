package mihomo

import (
	"encoding/json"
	"strings"
	"testing"

	"netpilot/internal/core"
	"netpilot/internal/model"
)

func TestRuleSkipsUnsafeValues(t *testing.T) {
	r := &core.PlanRule{
		Domains:  []string{"a.com", "b,c.com"},
		Keywords: []string{"good", "bad(x)"},
		Regexps:  []string{`^a{1,3}\.com$`, `^ok\.com$`},
	}
	got := rule(r, "proxy")
	want := `OR,((DOMAIN,a.com),(DOMAIN-KEYWORD,good),(DOMAIN-REGEX,^ok\.com$)),proxy`
	if got != want {
		t.Fatalf("правило:\n%s\nждали:\n%s", got, want)
	}

	// Все условия на домены отброшены: правило не должно превратиться
	// в MATCH (весь трафик) или в «только порт 443».
	r = &core.PlanRule{Regexps: []string{`a{1,3}`}, Ports: []int{443}}
	if got := rule(r, "REJECT"); got != "" {
		t.Fatalf("ждали пропуск правила, получили %q", got)
	}

	// Без доменных условий изначально — обычное правило.
	r = &core.PlanRule{Ports: []int{443}}
	if got := rule(r, "DIRECT"); got != "DST-PORT,443,DIRECT" {
		t.Fatalf("правило по порту: %q", got)
	}
}

func TestCheckReportsUnsafeValues(t *testing.T) {
	cfg := model.Defaults()
	cfg.Rules = []model.Rule{
		{ID: "1", Name: "Плохое", Enabled: true, What: model.What{Domains: []string{"ok.com", "regexp:a{1,3}"}}},
		{ID: "2", Name: "Выключенное", Enabled: false, What: model.What{Domains: []string{"x,y"}}},
		{ID: "3", Name: "Хорошее", Enabled: true, What: model.What{Domains: []string{"ok.com"}}},
	}
	c := New(t.TempDir())
	issues := c.Check(cfg)
	if len(issues) != 1 || !strings.Contains(issues[0].Message, "Плохое") || !strings.Contains(issues[0].Message, "a{1,3}") {
		t.Fatalf("замечания: %+v", issues)
	}
	// Validate показывает их вместе с остальными.
	found := false
	for _, is := range core.Validate(c, cfg) {
		found = found || strings.Contains(is.Message, "Плохое")
	}
	if !found {
		t.Fatal("Validate не показал замечание mihomo")
	}

	// Конфиг собирается, плохое значение в него не попадает, хорошее — попадает.
	data, err := c.Render(cfg, core.Env{OS: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	var conf struct {
		Rules []string `json:"rules"`
	}
	if err := json.Unmarshal(data, &conf); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(conf.Rules, "\n")
	if strings.Contains(all, "a{1") || !strings.Contains(all, "ok.com") {
		t.Fatalf("правила:\n%s", all)
	}
}

// Ключ Clash API и IPv6-адреса устройств попадают в конфиг mihomo.
func TestRenderSecretAndDeviceIPv6(t *testing.T) {
	cfg := model.Defaults()
	cfg.Devices = []model.Device{{ID: "tv", IP: "192.168.1.50", Route: "direct"}}
	env := core.Env{OS: "linux", ClashAddr: "127.0.0.1:9090", ClashSecret: "s3", DeviceIPv6: map[string][]string{"tv": {"2001:db8::50"}}}
	data, err := New(t.TempDir()).Render(cfg, env)
	if err != nil {
		t.Fatal(err)
	}
	var conf struct {
		Secret string   `json:"secret"`
		Rules  []string `json:"rules"`
	}
	if err := json.Unmarshal(data, &conf); err != nil {
		t.Fatal(err)
	}
	if conf.Secret != "s3" {
		t.Errorf("ключ API: %q", conf.Secret)
	}
	if all := strings.Join(conf.Rules, "\n"); !strings.Contains(all, "SRC-IP-CIDR,2001:db8::50/128") {
		t.Errorf("нет IPv6-адреса устройства в правилах:\n%s", all)
	}
}
