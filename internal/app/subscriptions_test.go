package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"netpilot/internal/model"
	"netpilot/internal/store"
)

func TestSubscriptionKeepsUniqueIDs(t *testing.T) {
	// Два сервера с одинаковыми именем и адресом, но разными портами.
	body := "trojan://pw@de.example.com:443#DE\ntrojan://pw@de.example.com:8443#DE\n"
	fail := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, "нет", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	st, err := store.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Update(func(c *model.Config) error {
		c.Subscriptions = []model.Subscription{{ID: "s", Name: "Провайдер", URL: srv.URL, Interval: 24}}
		return nil
	})
	a := &App{Store: st, DataDir: t.TempDir()}
	ctx := context.Background()

	for range 2 { // второе обновление сопоставляет серверы с прежними id
		if _, _, err := a.UpdateSubscription(ctx, "s"); err != nil {
			t.Fatal(err)
		}
		ids := map[string]bool{}
		for _, n := range st.Get().Nodes {
			if ids[n.ID] {
				t.Fatalf("повторяющийся id %q", n.ID)
			}
			ids[n.ID] = true
		}
		if len(ids) != 2 {
			t.Fatalf("серверов %d, ожидалось 2", len(ids))
		}
	}

	// Неудачное обновление не сдвигает время: подписка остаётся «к обновлению».
	updated := st.Get().Subscriptions[0].UpdatedAt
	fail = true
	if _, _, err := a.UpdateSubscription(ctx, "s"); err == nil {
		t.Fatal("ошибка загрузки не замечена")
	}
	sub := st.Get().Subscriptions[0]
	if sub.UpdatedAt != updated || sub.LastError == "" {
		t.Fatalf("после ошибки: updatedAt %d→%d, ошибка %q", updated, sub.UpdatedAt, sub.LastError)
	}
}
