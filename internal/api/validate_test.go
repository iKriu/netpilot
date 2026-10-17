package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"netpilot/internal/app"
	"netpilot/internal/auth"
	"netpilot/internal/dhcp"
	"netpilot/internal/model"
	"netpilot/internal/store"
)

// newAppServer — сервер с настоящим хранилищем и входом. App собран вручную,
// без app.New: тому нужны ядра, nftables и роутер, а здесь проверяется только
// запись конфигурации.
func newAppServer(t *testing.T, cfg func(*model.Config)) (http.Handler, *app.App, map[string]string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		if err := st.Update(func(c *model.Config) error { cfg(c); return nil }); err != nil {
			t.Fatal(err)
		}
	}
	a := &app.App{Store: st, DHCP: dhcp.None{}, DataDir: dir}
	au, err := auth.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tok, err := au.Setup("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	h := New(a, au, nil, fstest.MapFS{"index.html": {Data: []byte("ui")}})
	return h, a, map[string]string{"Cookie": auth.CookieName + "=" + tok}
}

func TestValidateOnWrite(t *testing.T) {
	h, a, ck := newAppServer(t, nil)
	settings, _ := json.Marshal(func() model.Settings { s := a.Store.Get().Settings; s.Mode = "magic"; return s }())
	cases := []struct{ name, method, path, body, want string }{
		{"узел без адреса", "POST", "/api/nodes", `{"name":"a","protocol":"vless","port":443}`, "не указан адрес"},
		{"узел с портом 0", "POST", "/api/nodes", `{"name":"a","protocol":"vless","server":"x.example"}`, "неверный порт"},
		{"перевод строки", "POST", "/api/nodes", `{"name":"a\nb","protocol":"vless","server":"x.example","port":1}`, "недопустимые символы"},
		{"правило с неизвестным действием", "POST", "/api/rules", `{"name":"r","where":{"action":"teleport"}}`, "неизвестное действие"},
		{"правило с плохим портом", "POST", "/api/rules", `{"name":"r","what":{"ports":["99999"]},"where":{"action":"direct"}}`, "неверный порт"},
		{"устройство с плохим IP", "POST", "/api/devices", `{"name":"d","ip":"300.1.1.1"}`, "не IPv4-адрес"},
		{"устройство с плохим маршрутом", "POST", "/api/devices", `{"name":"d","route":"moon"}`, "неизвестный маршрут"},
		{"подписка без http", "POST", "/api/subscriptions", `{"name":"s","url":"ftp://x"}`, "http://"},
		{"группа с плохим фильтром", "POST", "/api/groups", `{"name":"g","filter":"("}`, "ошибка в фильтре"},
		{"настройки", "PUT", "/api/settings", string(settings), "неизвестное значение"},
	}
	for _, c := range cases {
		w := do(h, req{method: c.method, path: c.path, body: c.body, hdr: ck})
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), c.want) {
			t.Errorf("%s: %d %s", c.name, w.Code, w.Body)
		}
	}
	if n := len(a.Store.Get().Nodes) + len(a.Store.Get().Rules) + len(a.Store.Get().Devices); n != 0 {
		t.Fatalf("неверные объекты сохранились: %d", n)
	}
	if a.Store.Get().Settings.Mode != "proxy" {
		t.Fatal("неверные настройки сохранились")
	}
	// Верный объект проходит.
	if w := do(h, req{method: "POST", path: "/api/nodes", body: `{"name":"ok","protocol":"vless","server":"x.example","port":443}`, hdr: ck}); w.Code != http.StatusOK {
		t.Fatalf("верный узел: %d %s", w.Code, w.Body)
	}
}

func TestBulkDevicesValidation(t *testing.T) {
	h, a, ck := newAppServer(t, func(c *model.Config) {
		c.Nodes = []model.Node{{ID: "n1", Name: "n1", Protocol: "vless", Server: "x", Port: 1}}
		c.Devices = []model.Device{{ID: "d1", Name: "d1"}}
	})
	for _, body := range []string{`{"ids":["d1"],"route":"moon"}`, `{"ids":["d1"],"route":"proxy","routeTarget":"nope"}`} {
		if w := do(h, req{method: "PUT", path: "/api/devices/bulk", body: body, hdr: ck}); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d %s", body, w.Code, w.Body)
		}
	}
	if d := a.Store.Get().Devices[0]; d.Route != "" || d.RouteTarget != "" {
		t.Fatalf("неверный маршрут сохранился: %+v", d)
	}
	if w := do(h, req{method: "PUT", path: "/api/devices/bulk", body: `{"ids":["d1"],"route":"proxy","routeTarget":"n1"}`, hdr: ck}); w.Code != http.StatusOK {
		t.Fatalf("верный маршрут: %d %s", w.Code, w.Body)
	}
	if d := a.Store.Get().Devices[0]; d.Route != "proxy" || d.RouteTarget != "n1" {
		t.Fatalf("маршрут не сохранился: %+v", d)
	}
}

// referenced — узел n1 используется везде, где на него можно сослаться.
func referenced(c *model.Config) {
	c.Settings.DefaultTarget = "n1"
	c.Nodes = []model.Node{
		{ID: "n1", Name: "Первый", Protocol: "vless", Server: "a", Port: 1, SubscriptionID: "s1"},
		{ID: "n2", Name: "Второй", Protocol: "vless", Server: "b", Port: 1},
	}
	c.Groups = []model.Group{{ID: "g1", Name: "Ручная", Type: "select", NodeIDs: []string{"n1", "n2"}}}
	c.Rules = []model.Rule{{ID: "r1", Name: "Игры", Where: model.Where{Action: "proxy", Target: "n1"}}}
	c.Devices = []model.Device{{ID: "d1", Name: "ТВ", Route: "proxy", RouteTarget: "n1"}}
	c.Subscriptions = []model.Subscription{{ID: "s1", Name: "Подписка", URL: "https://sub.example"}}
}

func TestDeleteReferencedNode(t *testing.T) {
	h, a, ck := newAppServer(t, referenced)
	w := do(h, req{method: "DELETE", path: "/api/nodes/n1", hdr: ck})
	if w.Code != http.StatusConflict {
		t.Fatalf("удаление используемого сервера: %d %s", w.Code, w.Body)
	}
	var out struct {
		Error      string   `json:"error"`
		References []string `json:"references"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.References) != 4 || !strings.Contains(out.Error, "используется") {
		t.Fatalf("ответ 409: %+v", out)
	}
	if len(a.Store.Get().Nodes) != 2 {
		t.Fatal("сервер удалён без force")
	}

	if w := do(h, req{method: "DELETE", path: "/api/nodes/n1?force=1", hdr: ck}); w.Code != http.StatusOK {
		t.Fatalf("удаление с force: %d %s", w.Code, w.Body)
	}
	checkDetached(t, a.Store.Get())
}

func TestPurgeSubscriptionReferences(t *testing.T) {
	h, a, ck := newAppServer(t, referenced)
	if w := do(h, req{method: "POST", path: "/api/subscriptions/s1/purge", hdr: ck}); w.Code != http.StatusConflict {
		t.Fatalf("удаление подписки с используемыми серверами: %d %s", w.Code, w.Body)
	}
	if len(a.Store.Get().Subscriptions) != 1 {
		t.Fatal("подписка удалена без force")
	}
	if w := do(h, req{method: "POST", path: "/api/subscriptions/s1/purge?force=1", hdr: ck}); w.Code != http.StatusOK {
		t.Fatalf("удаление подписки с force: %d %s", w.Code, w.Body)
	}
	c := a.Store.Get()
	if len(c.Subscriptions) != 0 {
		t.Fatal("подписка не удалена")
	}
	checkDetached(t, c)
}

func TestDeleteGroupReferences(t *testing.T) {
	h, a, ck := newAppServer(t, func(c *model.Config) {
		referenced(c)
		c.Settings.DefaultTarget = "g1"
	})
	if w := do(h, req{method: "DELETE", path: "/api/groups/g1", hdr: ck}); w.Code != http.StatusConflict {
		t.Fatalf("удаление активной группы: %d %s", w.Code, w.Body)
	}
	if w := do(h, req{method: "DELETE", path: "/api/groups/g1?force=1", hdr: ck}); w.Code != http.StatusOK {
		t.Fatalf("удаление группы с force: %d %s", w.Code, w.Body)
	}
	if c := a.Store.Get(); len(c.Groups) != 0 || c.Settings.DefaultTarget != "" {
		t.Fatalf("группа не удалена или осталась ссылка: %+v", c.Settings.DefaultTarget)
	}
	// Без ссылок удаление проходит сразу.
	if w := do(h, req{method: "DELETE", path: "/api/nodes/n2", hdr: ck}); w.Code != http.StatusOK {
		t.Fatalf("удаление свободного сервера: %d %s", w.Code, w.Body)
	}
}

// checkDetached — n1 удалён, и ни одной ссылки на него не осталось.
func checkDetached(t *testing.T, c *model.Config) {
	t.Helper()
	if slices.ContainsFunc(c.Nodes, func(n model.Node) bool { return n.ID == "n1" }) {
		t.Fatal("сервер не удалён")
	}
	if !slices.Equal(c.Groups[0].NodeIDs, []string{"n2"}) {
		t.Errorf("состав группы: %v", c.Groups[0].NodeIDs)
	}
	if c.Rules[0].Where.Target != "" || c.Devices[0].RouteTarget != "" || c.Settings.DefaultTarget != "" {
		t.Errorf("остались ссылки: правило %q, устройство %q, по умолчанию %q",
			c.Rules[0].Where.Target, c.Devices[0].RouteTarget, c.Settings.DefaultTarget)
	}
	if refs := model.References(c, "n1"); len(refs) != 0 {
		t.Errorf("References после удаления: %v", refs)
	}
}
