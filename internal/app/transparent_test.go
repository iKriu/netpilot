package app

import (
	"net/netip"
	"slices"
	"sync"
	"testing"

	"netpilot/internal/model"
)

func TestFirewallSpecSkipsBadAddrs(t *testing.T) {
	a := &App{}
	a.net.v6 = map[string][]string{"tv": {"2001:db8::5", "не-адрес"}}
	cfg := &model.Config{Devices: []model.Device{
		{ID: "tv", IP: "192.168.1.5", Route: "block"},
		{ID: "gone", IP: "", Route: "block"},
		{ID: "typo", IP: "192.168.1.300", Route: "direct"},
		{ID: "zone", IP: "fe80::1%eth0", Route: "direct"},
		{ID: "pc", IP: "192.168.1.7", Route: "direct"},
	}}
	spec := a.firewallSpec(cfg)
	if want := []string{"2001:db8::5", "192.168.1.5"}; !slices.Equal(spec.Block, want) {
		t.Errorf("Block = %v, ожидалось %v", spec.Block, want)
	}
	if want := []string{"192.168.1.7"}; !slices.Equal(spec.Direct, want) {
		t.Errorf("Direct = %v, ожидалось %v", spec.Direct, want)
	}
}

// Запасной DNS никогда не указывает на сам сервер: иначе DNAT на себя — петля.
func TestFallbackUpstream(t *testing.T) {
	self := netip.MustParseAddr("192.168.0.73")
	isLocal := func(ip netip.Addr) bool { return ip == self }
	cases := []struct{ sys, gw, want string }{
		{"192.168.0.1", "192.168.0.1", "192.168.0.1"},  // DNS системы
		{"", "192.168.0.1", "192.168.0.1"},             // нет DNS системы — роутер
		{"192.168.0.73", "192.168.0.1", "192.168.0.1"}, // DNS системы — мы сами
		{"", "192.168.0.73", "1.1.1.1"},                // и шлюз — мы сами: публичный
		{"", "", "1.1.1.1"},
		{"127.0.0.53", "мусор", "1.1.1.1"},
	}
	for _, c := range cases {
		if got := fallbackUpstream(c.sys, c.gw, isLocal); got != c.want {
			t.Errorf("fallbackUpstream(%q, %q) = %q, ожидалось %q", c.sys, c.gw, got, c.want)
		}
	}
}

// stubFallback подменяет nftables запасного DNS и возвращает журнал вызовов
// ("" — выключение, адрес — включение).
func stubFallback(t *testing.T, resolver string) func() []string {
	t.Helper()
	var mu sync.Mutex
	var calls []string
	pOS, pSet, pRes, pSys, pGW := fallbackOS, setDNSFallback, localResolver, systemDNS, defaultGateway
	fallbackOS = true
	setDNSFallback = func(up string) error { mu.Lock(); calls = append(calls, up); mu.Unlock(); return nil }
	localResolver = func() string { return resolver }
	systemDNS = func() string { return "" }
	defaultGateway = func() string { return "192.168.0.1" }
	t.Cleanup(func() {
		fallbackOS, setDNSFallback, localResolver, systemDNS, defaultGateway = pOS, pSet, pRes, pSys, pGW
	})
	return func() []string { mu.Lock(); defer mu.Unlock(); return slices.Clone(calls) }
}

func enabledFallbacks(calls []string) []string {
	var out []string
	for _, c := range calls {
		if c != "" {
			out = append(out, c)
		}
	}
	return out
}

// newTransparentApp — ядро запущено (в режиме прокси-сервера: прозрачный режим
// на этой системе может быть недоступен), затем режим переключён на прозрачный.
func newTransparentApp(t *testing.T) (*App, *fakeProc) {
	t.Helper()
	stubProbe(t) // без сети: выбор DNS при запуске ничего не ждёт
	a, fp := newWatchApp(t)
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	_ = a.Store.Update(func(c *model.Config) error { c.Settings.Mode = "transparent"; return nil })
	return a, fp
}

// Ядро не запустилось (например, при автозапуске) — перехват не включался ни
// разу, но запасной DNS всё равно нужен: DNS устройств указан на шлюз.
func TestFallbackWhenCoreNeverStarted(t *testing.T) {
	calls := stubFallback(t, "")
	stubProbe(t)
	a, fp := newWatchApp(t)
	_ = a.Store.Update(func(c *model.Config) error { c.Settings.Mode = "transparent"; return nil })
	fp.setFailing(true)
	if err := a.Start(); err == nil {
		t.Fatal("запуск должен был не удаться")
	}
	if got := enabledFallbacks(calls()); !slices.Equal(got, []string{"192.168.0.1"}) {
		t.Fatalf("включения запасного DNS: %v", got)
	}
}

// Остановка в панели — запасной DNS включается; остановка с немедленным
// запуском (hold: применение настроек ядру без перезагрузки) — не мигает.
func TestFallbackStopVsRestart(t *testing.T) {
	calls := stubFallback(t, "")
	a, _ := newTransparentApp(t)
	a.mu.Lock()
	a.tp.hold = true
	_ = a.stopLocked()
	a.tp.hold = false
	a.mu.Unlock()
	if got := enabledFallbacks(calls()); len(got) != 0 {
		t.Fatalf("при остановке ради перезапуска запасной DNS включался: %v", got)
	}
	if err := a.Stop(); err != nil {
		t.Fatal(err)
	}
	if got := enabledFallbacks(calls()); !slices.Equal(got, []string{"192.168.0.1"}) {
		t.Fatalf("после остановки: %v", got)
	}
}

// Свой DNS-сервер на этом компьютере (Pi-hole, dnsmasq, OpenWrt) не перехватываем.
func TestFallbackSkipsLocalResolver(t *testing.T) {
	calls := stubFallback(t, "на этом сервере уже работает DNS-сервер")
	a, _ := newTransparentApp(t)
	if err := a.Stop(); err != nil {
		t.Fatal(err)
	}
	if got := enabledFallbacks(calls()); len(got) != 0 {
		t.Fatalf("запасной DNS включён поверх локального DNS-сервера: %v", got)
	}
}

// Выключение не полагается на флаг: таблица удаляется, даже если флаг сброшен.
func TestFallbackOffIdempotent(t *testing.T) {
	calls := stubFallback(t, "")
	a, _ := newWatchApp(t)
	a.mu.Lock()
	a.dnsFallbackLocked(false)
	a.mu.Unlock()
	if got := calls(); !slices.Equal(got, []string{""}) {
		t.Fatalf("вызовы: %v", got)
	}
}
