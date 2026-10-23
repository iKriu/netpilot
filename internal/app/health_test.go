package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"netpilot/internal/model"
	"netpilot/internal/store"
)

// stubProbe подменяет проверку DoH: отвечают только серверы из ok.
func stubProbe(t *testing.T, ok ...string) {
	t.Helper()
	prev := probeDoH
	probeDoH = func(_ context.Context, s string) error {
		for _, o := range ok {
			if strings.Contains(s, o) {
				return nil
			}
		}
		return errors.New("не отвечает")
	}
	t.Cleanup(func() { probeDoH = prev })
}

func newHealthApp(t *testing.T, dns string) *App {
	t.Helper()
	st, err := store.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Update(func(c *model.Config) error { c.Settings.DNSDirect = dns; return nil })
	return &App{Store: st, DataDir: t.TempDir()}
}

// Зарубежные DoH — в приоритете, Яндекс — последний запасной.
func TestPickDNSPriority(t *testing.T) {
	if !strings.Contains(model.DirectDNSAuto[len(model.DirectDNSAuto)-1], "77.88.8.8") {
		t.Fatal("Яндекс должен быть последним в списке")
	}
	a := newHealthApp(t, model.DNSAuto)

	stubProbe(t, "1.1.1.1", "77.88.8.8")
	a.pickDNS(context.Background())
	if got := a.directDNS(); !strings.Contains(got, "1.1.1.1") {
		t.Fatalf("отвечает Cloudflare — должен быть он, выбран %s", got)
	}

	stubProbe(t, "9.9.9.9", "77.88.8.8")
	a.pickDNS(context.Background())
	if got := a.directDNS(); !strings.Contains(got, "9.9.9.9") {
		t.Fatalf("Cloudflare и Google не отвечают — должен быть Quad9, выбран %s", got)
	}

	stubProbe(t, "77.88.8.8")
	a.pickDNS(context.Background())
	if got := a.directDNS(); !strings.Contains(got, "77.88.8.8") {
		t.Fatalf("зарубежные недоступны — должен быть Яндекс, выбран %s", got)
	}

	// В env для ядра попадает выбранный сервер.
	if a.env().DNSDirect != a.directDNS() {
		t.Fatal("env.DNSDirect не совпадает с выбранным DNS")
	}
}

// Свой DNS в настройках не трогаем — автоматический выбор только при «auto».
func TestPickDNSManual(t *testing.T) {
	a := newHealthApp(t, "tls://dns.google")
	stubProbe(t, "77.88.8.8")
	a.pickDNS(context.Background())
	if a.dnsIdx.Load() != 0 {
		t.Fatal("при своём DNS выбор не должен меняться")
	}
}

// Проверка ядра идёт через /dns/query Clash API со случайным именем.
func TestCoreDNSWorks(t *testing.T) {
	status := http.StatusOK
	var names []string
	srv := httptest.NewServer(withSecret(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/dns/query" {
			http.NotFound(w, r)
			return
		}
		names = append(names, r.URL.Query().Get("name"))
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{}`))
	})))
	defer srv.Close()
	a := newHealthApp(t, model.DNSAuto)
	a.active = &fakeClashCore{api: testClash(srv)}

	if ok, checked, _ := a.coreDNSWorks(context.Background()); !ok || !checked {
		t.Fatalf("ответ 200: ok=%v checked=%v", ok, checked)
	}
	status = http.StatusInternalServerError
	if ok, checked, _ := a.coreDNSWorks(context.Background()); ok || !checked {
		t.Fatalf("ошибка DNS в ядре: ok=%v checked=%v", ok, checked)
	}
	status = http.StatusNotFound
	if _, checked, _ := a.coreDNSWorks(context.Background()); checked {
		t.Fatal("нет метода /dns/query — проверка не должна засчитываться")
	}
	if len(names) < 2 || names[0] == names[1] || !strings.HasSuffix(names[0], ".example.com") {
		t.Fatalf("имена должны быть случайными (мимо кэша): %v", names)
	}

	a.active = nil
	if _, checked, _ := a.coreDNSWorks(context.Background()); checked {
		t.Fatal("ядро остановлено — проверять нечего")
	}
}

// NXDOMAIN на случайное имя — это ответ DNS, а не сбой; таймаут — сбой.
func TestDNSQueryAnswered(t *testing.T) {
	cases := []struct {
		status      int
		body        string
		ok, checked bool
	}{
		{200, `{"Status":3}`, true, true},
		{500, `{"message":"rcode: name error"}`, true, true},
		{500, `{"message":"lookup np-1.example.com: NXDOMAIN"}`, true, true},
		{500, `{"message":"context deadline exceeded"}`, false, true},
		{500, `{"message":"dial udp 1.1.1.1:53: i/o timeout"}`, false, true},
		{500, `{}`, false, true},
		{502, ``, false, true},
		{404, ``, false, false},
		{405, ``, false, false},
	}
	for _, c := range cases {
		ok, checked := dnsQueryAnswered(c.status, []byte(c.body))
		if ok != c.ok || checked != c.checked {
			t.Errorf("%d %s: ok=%v checked=%v, ожидалось %v %v", c.status, c.body, ok, checked, c.ok, c.checked)
		}
	}
}

// Выбор DNS не ждёт серверы по очереди: все проверяются сразу, побеждает
// отвечающий с наивысшим приоритетом.
func TestFirstWorkingDNSParallel(t *testing.T) {
	prev, prevT := probeDoH, probeTimeout
	probeTimeout = 300 * time.Millisecond
	probeDoH = func(ctx context.Context, s string) error {
		if strings.Contains(s, "77.88.8.8") || strings.Contains(s, "9.9.9.9") {
			return nil
		}
		<-ctx.Done() // «висит» до таймаута
		return ctx.Err()
	}
	t.Cleanup(func() { probeDoH, probeTimeout = prev, prevT })
	start := time.Now()
	i := firstWorkingDNS(context.Background(), 0, len(model.DirectDNSAuto))
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("выбор занял %v — проверки идут не параллельно", took)
	}
	if i < 0 || !strings.Contains(model.DirectDNSAuto[i], "9.9.9.9") {
		t.Fatalf("ожидался Quad9 (приоритетнее Яндекса), выбран %d", i)
	}
	if firstWorkingDNS(context.Background(), 3, 3) != -1 {
		t.Fatal("пустой диапазон")
	}
}

// Напрямую не отвечает ни один DoH — у сервера нет интернета: ядро не
// перезапускается (иначе авария у провайдера исчерпала бы перезапуски).
func TestDNSUnhealthyNoUplinkNoRestart(t *testing.T) {
	stubProbe(t)
	a, fp := newWatchApp(t)
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	gen := a.coreGen
	a.mu.Unlock()
	a.dnsUnhealthy(context.Background(), gen)
	if fp.startCount() != 1 || !a.Status().Running {
		t.Fatalf("ядро перезапущено без интернета: запусков %d", fp.startCount())
	}
}

// Зависшее ядро перезапускается сразу, но не чаще healthRestartGaps, и эти
// перезапуски не идут в счёт падений (restartMaxFails).
func TestDNSUnhealthyRestartBackoff(t *testing.T) {
	stubProbe(t, "1.1.1.1") // напрямую DNS отвечает — значит, зависло ядро
	a, fp := newWatchApp(t)
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	unhealthy := func() {
		a.mu.Lock()
		gen := a.coreGen
		a.mu.Unlock()
		a.dnsUnhealthy(context.Background(), gen)
	}
	unhealthy()
	if fp.startCount() != 2 || !a.Status().Running {
		t.Fatalf("после первого сбоя: запусков %d", fp.startCount())
	}
	unhealthy() // сразу же снова — рано
	if fp.startCount() != 2 {
		t.Fatalf("перезапуск чаще паузы: запусков %d", fp.startCount())
	}
	a.mu.Lock()
	a.tp.healthRestarts[0] = time.Now().Add(-healthRestartGaps[0] - time.Second)
	fails := len(a.restart.fails)
	a.mu.Unlock()
	unhealthy()
	if fp.startCount() != 3 {
		t.Fatalf("после паузы перезапуска нет: запусков %d", fp.startCount())
	}
	if fails != 0 {
		t.Fatalf("перезапуски по проверке живучести засчитаны как падения: %d", fails)
	}
}

// Устаревший номер запуска: ядро остановили, пока шли пробы, — reloadIfGen
// ничего не делает (раньше Apply() запустил бы его снова).
func TestReloadIfGenStale(t *testing.T) {
	stubProbe(t)
	a, fp := newWatchApp(t)
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	gen := a.coreGen
	a.mu.Unlock()
	if err := a.Stop(); err != nil {
		t.Fatal(err)
	}
	a.reloadIfGen(gen)
	if fp.startCount() != 1 || a.Status().Running {
		t.Fatalf("остановленное ядро запущено проверкой: запусков %d", fp.startCount())
	}
	// Текущий запуск без перезагрузки на лету — перезапуск.
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	gen = a.coreGen
	a.mu.Unlock()
	a.reloadIfGen(gen)
	if fp.startCount() != 3 || !a.Status().Running {
		t.Fatalf("ожидался перезапуск: запусков %d", fp.startCount())
	}
}

func TestHealthRestartWait(t *testing.T) {
	a := &App{}
	now := time.Now()
	if a.healthRestartWaitLocked(now) != 0 {
		t.Fatal("первый перезапуск — сразу")
	}
	a.tp.healthRestarts = []time.Time{now.Add(-time.Minute)}
	if w := a.healthRestartWaitLocked(now); w <= 0 || w > healthRestartGaps[0] {
		t.Fatalf("после одного: %v", w)
	}
	a.tp.healthRestarts = []time.Time{now.Add(-3 * time.Hour)}
	if a.healthRestartWaitLocked(now) != 0 || len(a.tp.healthRestarts) != 0 {
		t.Fatal("старые перезапуски не забываются")
	}
}
