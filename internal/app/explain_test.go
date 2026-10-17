package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"netpilot/internal/model"
	"netpilot/internal/store"
)

func TestExplainRoute(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(dir, "lists"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "lists", "refilter-domains.txt"), []byte("rutracker.org\ninstagram.com\n"), 0o644)
	_ = st.Update(func(c *model.Config) error {
		c.Settings.DefaultAction = "direct"
		c.Settings.DefaultTarget = "eu"
		c.Groups = []model.Group{{ID: "eu", Name: "Европа (авто)", Type: "auto"}, {ID: "tor", Name: "Торренты", Type: "auto"}}
		c.Devices = []model.Device{{ID: "tv", Name: "Samsung TV", IP: "192.168.0.27", Route: "proxy", RouteTarget: "tor"}}
		c.Rules = []model.Rule{
			{ID: "1", Name: "Торренты напрямую", Enabled: true, What: model.What{Protocols: []string{"bittorrent"}}, Where: model.Where{Action: "direct"}},
			{ID: "2", Name: "Parsec", Enabled: true, What: model.What{Domains: []string{"parsec.app"}}, Where: model.Where{Action: "proxy"}},
			{ID: "3", Name: "Заблокированное", Enabled: true, What: model.What{Lists: []string{"refilter-domains"}}, Where: model.Where{Action: "proxy", Target: "tor"}},
			{ID: "4", Name: "Выключенное", Enabled: false, What: model.What{Domains: []string{"ya.ru"}}, Where: model.Where{Action: "block"}},
		}
		return nil
	})
	a := &App{Store: st, DataDir: dir}
	ctx := context.Background()

	cases := []struct {
		host, source, action, target, rule string
	}{
		{"https://api.parsec.app/v1", "", "proxy", "Европа (авто)", "Parsec"},
		{"rutracker.org", "", "proxy", "Торренты", "Заблокированное"},
		{"bt.rutracker.org", "", "proxy", "Торренты", "Заблокированное"},
		{"ya.ru", "", "direct", "", ""},
		{"ya.ru", "192.168.0.27", "proxy", "Торренты", ""}, // маршрут устройства важнее правил
	}
	for _, c := range cases {
		ex := a.ExplainRoute(ctx, c.host, c.source)
		if ex.Action != c.action || ex.Target != c.target || ex.Rule != c.rule {
			t.Errorf("%s от %q: %s / %q / %q (%s)", c.host, c.source, ex.Action, ex.Target, ex.Rule, ex.Reason)
		}
	}
	// Правило по протоколу к сайту не относится — без пометок.
	if ex := a.ExplainRoute(ctx, "ya.ru", ""); len(ex.Notes) != 0 {
		t.Errorf("лишние пометки: %v", ex.Notes)
	}
}

func TestStatsByDay(t *testing.T) {
	a := &App{DataDir: t.TempDir()}
	now := time.Date(2026, 10, 1, 21, 0, 0, 0, time.Local)
	a.recordTraffic(now, "192.168.0.54", true, 10, 1000)
	a.recordTraffic(now, "192.168.0.54", false, 5, 500)
	a.recordTraffic(now, "192.168.0.27", false, 0, 300)
	a.recordPeak(now, 4000)
	a.recordPeak(now, 2000)
	a.saveStats(true)

	b := &App{DataDir: a.DataDir} // как после перезапуска
	b.stats.load(b.statsPath())
	d := b.stats.days[len(b.stats.days)-1]
	if d.Down != 1800 || d.ProxyDown != 1000 || d.DirectDown != 800 || d.Peak != 4000 {
		t.Fatalf("день: %+v", d)
	}
	if v := d.Devices["192.168.0.54"]; v != [2]int64{15, 1500} {
		t.Fatalf("устройство: %v", v)
	}
}
