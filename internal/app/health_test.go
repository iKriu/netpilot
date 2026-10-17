package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
