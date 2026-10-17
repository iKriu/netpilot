package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"netpilot/internal/core"
	"netpilot/internal/model"
	"netpilot/internal/store"
)

// fakeClash — Clash API: задержка каждого сервера задаётся тестом, -1 — не отвечает.
type fakeClash struct {
	mu       sync.Mutex
	delay    map[string]int // тег сервера → мс
	selected map[string]string
	inFlight atomic.Int32
	maxSeen  atomic.Int32
	calls    atomic.Int32
}

func (f *fakeClash) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := f.inFlight.Add(1)
	defer f.inFlight.Add(-1)
	if n > f.maxSeen.Load() {
		f.maxSeen.Store(n)
	}
	path := strings.TrimPrefix(r.URL.Path, "/proxies/")
	if tag, ok := strings.CutSuffix(path, "/delay"); ok {
		f.calls.Add(1)
		f.mu.Lock()
		d := f.delay[tag]
		f.mu.Unlock()
		if d <= 0 {
			w.WriteHeader(http.StatusGatewayTimeout)
			_, _ = w.Write([]byte(`{"message":"timeout"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]int{"delay": d})
		return
	}
	if r.Method == http.MethodPut {
		var body struct{ Name string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.selected[path] = body.Name
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}
}

func (f *fakeClash) set(tag string, ms int) {
	f.mu.Lock()
	f.delay[tag] = ms
	f.mu.Unlock()
}

func TestAutoSwitch(t *testing.T) {
	fc := &fakeClash{delay: map[string]int{"node-a": 300, "node-b": 80, "node-c": 150, "node-d": -1}, selected: map[string]string{}}
	srv := httptest.NewServer(withSecret(t, fc))
	defer srv.Close()
	api := testClash(srv)

	a := &App{DataDir: t.TempDir()}
	g := autoGroupInfo{Tag: "group-eu", Name: "Европа", Members: []string{"a", "b", "c", "d"}, InUse: true}
	ctx := context.Background()

	// Первый раз: перебор по одному, выбран быстрейший.
	a.checkGroup(ctx, api, g, "")
	if got := fc.selected["group-eu"]; got != "node-b" {
		t.Fatalf("после перебора выбран %q, ожидался node-b", got)
	}
	if fc.maxSeen.Load() != 1 {
		t.Fatalf("одновременных проверок: %d, должна быть одна", fc.maxSeen.Load())
	}

	// Текущий работает, но стал медленнее остальных — переключения нет.
	fc.set("node-b", 900)
	before := fc.calls.Load()
	a.checkGroup(ctx, api, g, "")
	if fc.calls.Load()-before != 1 {
		t.Fatalf("при работающем сервере проверок: %d, должна быть одна", fc.calls.Load()-before)
	}
	if got := fc.selected["group-eu"]; got != "node-b" {
		t.Fatalf("переключился с работающего сервера на %q", got)
	}

	// Один таймаут — ещё не повод.
	fc.set("node-b", -1)
	a.checkGroup(ctx, api, g, "")
	if got := fc.selected["group-eu"]; got != "node-b" {
		t.Fatalf("переключился после одного таймаута на %q", got)
	}
	// Второй подряд — переключение на первый ответивший в порядке прошлых задержек (c: 150 мс).
	a.checkGroup(ctx, api, g, "")
	if got := fc.selected["group-eu"]; got != "node-c" {
		t.Fatalf("после двух таймаутов выбран %q, ожидался node-c", got)
	}
	if fc.maxSeen.Load() != 1 {
		t.Fatalf("одновременных проверок: %d", fc.maxSeen.Load())
	}

	// Выбор сохраняется и передаётся ядру.
	b := &App{DataDir: a.DataDir}
	if got := b.autoSelected()["group-eu"]; got != "node-c" {
		t.Fatalf("после перезапуска выбор %q", got)
	}
}

func TestAutoGroupsInUse(t *testing.T) {
	cfg := &model.Config{
		Nodes:  []model.Node{{ID: "a"}, {ID: "b"}},
		Groups: []model.Group{{ID: "eu", Name: "Европа", Type: "auto"}, {ID: "x", Name: "Запас", Type: "auto"}, {ID: "m", Type: "select"}},
	}
	cfg.Settings.DefaultTarget = "eu"
	got, manual := map[string]bool{}, map[string]bool{}
	for _, g := range autoGroups(cfg) {
		got[g.Tag], manual[g.Tag] = g.InUse, g.Manual
	}
	if got[core.AutoTag] || !got[core.GroupTag("eu")] || got[core.GroupTag("x")] {
		t.Fatalf("используемые группы: %v", got)
	}
	// Ручная группа видна (в ней выбирают сервер), но NetPilot её не переключает.
	if !manual[core.GroupTag("m")] || manual[core.GroupTag("eu")] {
		t.Fatalf("ручные группы: %v", manual)
	}
	cfg.Settings.DefaultTarget = ""
	for _, g := range autoGroups(cfg) {
		if g.Tag == core.AutoTag && !g.InUse {
			t.Fatal("без выбранного сервера должна работать встроенная «Авто»")
		}
	}
}

func TestSelectInGroupWithoutCore(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Update(func(c *model.Config) error {
		c.Nodes = []model.Node{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}}
		c.Groups = []model.Group{{ID: "m", Name: "Ручная", Type: "select"}}
		return nil
	})
	a := &App{Store: st, DataDir: dir}
	if err := a.SelectInGroup(context.Background(), "m", "zzz"); err == nil {
		t.Fatal("сервер не из группы принят")
	}
	if err := a.SelectInGroup(context.Background(), "m", "b"); err != nil {
		t.Fatal(err)
	}
	if got := a.autoSelected()[core.GroupTag("m")]; got != core.NodeTag("b") {
		t.Fatalf("выбор не сохранён: %q", got)
	}
	groups, _ := a.AutoStatus()
	for _, g := range groups {
		if g.GroupID == "m" && (g.Selected != "b" || !g.Manual) {
			t.Fatalf("состояние ручной группы: %+v", g)
		}
	}
}
