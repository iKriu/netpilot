package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"netpilot/internal/core"
	"netpilot/internal/store"
)

// fakeClashCore — ядро, у которого есть только адрес Clash API.
type fakeClashCore struct {
	core.Core
	api core.Clash
}

func (f *fakeClashCore) ClashAPI() core.Clash { return f.api }

const testSecret = "test-secret"

// withSecret — поддельный Clash API, который, как настоящий, без ключа
// отвечает 401: так тесты ловят запрос, забывший заголовок.
func withSecret(t *testing.T, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testSecret {
			t.Errorf("%s %s без ключа API", r.Method, r.URL.Path)
			http.Error(w, `{"message":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// testClash — адрес тестового сервера с ключом.
func testClash(srv *httptest.Server) core.Clash {
	return core.Clash{Addr: strings.TrimPrefix(srv.URL, "http://"), Secret: testSecret}
}

type connRec struct {
	id, src, chain string
	up, down       int64
}

// fakeConns отдаёт /connections в формате sing-box/mihomo.
type fakeConns struct {
	mu        sync.Mutex
	upTotal   int64
	downTotal int64
	conns     []connRec
	noTotals  bool
}

func (f *fakeConns) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	type meta struct {
		Network  string `json:"network"`
		SourceIP string `json:"sourceIP"`
	}
	type conn struct {
		ID       string    `json:"id"`
		Metadata meta      `json:"metadata"`
		Upload   int64     `json:"upload"`
		Download int64     `json:"download"`
		Start    time.Time `json:"start"`
		Chains   []string  `json:"chains"`
	}
	body := map[string]any{}
	if !f.noTotals {
		body["uploadTotal"], body["downloadTotal"] = f.upTotal, f.downTotal
	}
	list := []conn{}
	for _, c := range f.conns {
		list = append(list, conn{ID: c.id, Metadata: meta{Network: "tcp", SourceIP: c.src}, Upload: c.up, Download: c.down, Start: time.Now(), Chains: []string{c.chain}})
	}
	body["connections"] = list
	_ = json.NewEncoder(w).Encode(body)
}

func (f *fakeConns) set(up, down int64, conns ...connRec) {
	f.mu.Lock()
	f.upTotal, f.downTotal, f.conns = up, down, conns
	f.mu.Unlock()
}

func newTrafficApp(t *testing.T, f *fakeConns) *App {
	srv := httptest.NewServer(withSecret(t, f))
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	st, err := store.Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	return &App{Store: st, DataDir: dir, active: &fakeClashCore{api: testClash(srv)}}
}

func checkStatsConsistent(t *testing.T, a *App) DayStats {
	t.Helper()
	d := a.Stats().Today
	if d.Up != d.ProxyUp+d.DirectUp || d.Down != d.ProxyDown+d.DirectDown {
		t.Fatalf("итоги не сходятся с разбивкой: %+v", d)
	}
	return d
}

func TestTrafficCountsClosedConnections(t *testing.T) {
	f := &fakeConns{}
	a := newTrafficApp(t, f)
	ctx := context.Background()
	const phone, tv = "192.168.1.10", "192.168.1.20"

	// Опрос 1: одно живое соединение телефона через прокси.
	f.set(100, 1000, connRec{"c1", phone, "proxy", 100, 1000})
	a.pollTraffic(ctx)

	// Опрос 2: c1 закрылось, дав ещё 50/500; телевизор открыл и закрыл короткое
	// соединение напрямую (5/50) — его ядро тоже посчитало; c2 живое.
	f.set(100+50+5+10, 1000+500+50+100, connRec{"c2", tv, "direct", 10, 100})
	a.pollTraffic(ctx)

	v := a.Connections()
	if v.Up != 165 || v.Down != 1650 {
		t.Fatalf("общий трафик %d/%d, ожидалось 165/1650", v.Up, v.Down)
	}
	d := checkStatsConsistent(t, a)
	if d.Up != 165 || d.Down != 1650 {
		t.Fatalf("статистика дня %d/%d, ожидалось 165/1650", d.Up, d.Down)
	}
	// Остаток (55/550) достался единственному закрытому соединению — телефону.
	bySrc := a.TrafficBySource()
	if got := bySrc[phone]; got.Up != 155 || got.Down != 1550 {
		t.Errorf("телефон %d/%d, ожидалось 155/1550", got.Up, got.Down)
	}
	if got := bySrc[tv]; got.Up != 10 || got.Down != 100 || got.Conns != 1 {
		t.Errorf("телевизор %+v", got)
	}
	if d.ProxyDown != 1550 || d.DirectDown != 100 {
		t.Errorf("прокси/напрямую: %d/%d", d.ProxyDown, d.DirectDown)
	}
}

func TestTrafficCounterReset(t *testing.T) {
	f := &fakeConns{}
	a := newTrafficApp(t, f)
	ctx := context.Background()
	f.set(1000, 5000, connRec{"a", "192.168.1.10", "proxy", 1000, 5000})
	a.pollTraffic(ctx)
	// Ядро перезапустилось: счётчики начались заново.
	f.set(30, 300, connRec{"b", "192.168.1.10", "proxy", 30, 300})
	a.pollTraffic(ctx)
	v := a.Connections()
	if v.Up != 1030 || v.Down != 5300 {
		t.Fatalf("после сброса счётчиков %d/%d, ожидалось 1030/5300", v.Up, v.Down)
	}
	d := checkStatsConsistent(t, a)
	if d.Up != 1030 || d.Down != 5300 {
		t.Fatalf("статистика %d/%d", d.Up, d.Down)
	}
}

func TestTrafficCarryWhenConnsAhead(t *testing.T) {
	f := &fakeConns{}
	a := newTrafficApp(t, f)
	ctx := context.Background()
	// Счётчик соединения успел вырасти, а общий — ещё нет.
	f.set(100, 100, connRec{"a", "192.168.1.10", "proxy", 120, 150})
	a.pollTraffic(ctx)
	f.set(200, 300, connRec{"a", "192.168.1.10", "proxy", 200, 300})
	a.pollTraffic(ctx)
	v := a.Connections()
	if v.Up != 200 || v.Down != 300 {
		t.Fatalf("итог %d/%d, ожидалось 200/300 (перебор не вычтен)", v.Up, v.Down)
	}
	checkStatsConsistent(t, a)
}

func TestTrafficWithoutTotals(t *testing.T) {
	f := &fakeConns{noTotals: true}
	a := newTrafficApp(t, f)
	ctx := context.Background()
	f.set(0, 0, connRec{"a", "192.168.1.10", "proxy", 10, 20})
	a.pollTraffic(ctx)
	f.set(0, 0, connRec{"a", "192.168.1.10", "proxy", 15, 40})
	a.pollTraffic(ctx)
	if v := a.Connections(); v.Up != 15 || v.Down != 40 {
		t.Fatalf("без общих счётчиков %d/%d, ожидалось 15/40", v.Up, v.Down)
	}
}

func TestSplitExact(t *testing.T) {
	items := []connMeta{{up: 0}, {up: 99}, {up: 9}}
	got := split(1001, items, func(m connMeta) int64 { return m.up })
	var sum int64
	for _, v := range got {
		sum += v
	}
	if sum != 1001 || got[1] <= got[2] || got[2] <= got[0] {
		t.Fatalf("split = %v", got)
	}
}
