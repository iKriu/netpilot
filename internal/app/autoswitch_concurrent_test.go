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
)

// waitFor ждёт условия до 5 секунд.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal(what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Долгий перебор одной группы не задерживает другую, а следующая проверка
// не запускает второй перебор той же группы.
func TestAutoGroupsConcurrent(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()

	var mu sync.Mutex
	calls := map[string]int{}
	selected := map[string]string{}
	srv := httptest.NewServer(withSecret(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/proxies/")
		if tag, ok := strings.CutSuffix(path, "/delay"); ok {
			mu.Lock()
			calls[tag]++
			mu.Unlock()
			if strings.HasPrefix(tag, "node-s") {
				<-release // сервер медленной группы «думает»
			}
			_ = json.NewEncoder(w).Encode(map[string]int{"delay": 100})
			return
		}
		var body struct{ Name string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		selected[path] = body.Name
		mu.Unlock()
	})))
	defer srv.Close()
	defer unblock() // до srv.Close: сервер ждёт завершения обработчиков
	api := testClash(srv)

	a := &App{DataDir: t.TempDir()}
	groups := []autoGroupInfo{
		{Tag: "group-slow", Name: "Медленная", Members: []string{"s1", "s2"}, InUse: true},
		{Tag: "group-fast", Name: "Быстрая", Members: []string{"f1"}, InUse: true},
	}
	ctx := context.Background()
	wg1 := a.checkGroups(ctx, api, groups, "")

	waitFor(t, "быстрая группа ждала медленную", func() bool {
		a.auto.mu.Lock()
		defer a.auto.mu.Unlock()
		return !a.auto.busy["group-fast"]
	})
	mu.Lock()
	got := selected["group-fast"]
	mu.Unlock()
	if got != "node-f1" {
		t.Fatalf("быстрая группа: выбран %q", got)
	}

	wg2 := a.checkGroups(ctx, api, groups, "") // медленная ещё идёт — её пропускаем
	unblock()
	wg1.Wait()
	wg2.Wait()

	mu.Lock()
	defer mu.Unlock()
	if calls["node-s1"] != 1 || calls["node-s2"] != 1 {
		t.Fatalf("медленная группа проверена повторно: %v", calls)
	}
	if calls["node-f1"] != 2 {
		t.Fatalf("быстрая группа: %d проверок, ожидалось 2", calls["node-f1"])
	}
}
