package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"netpilot/internal/auth"
)

// newTestServer — сервер без ядра: проверяемые пути до обработчиков,
// которым нужен app, не доходят.
func newTestServer(t *testing.T) (http.Handler, *auth.Auth) {
	t.Helper()
	au, err := auth.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(nil, au, nil, fstest.MapFS{"index.html": {Data: []byte("ui")}}), au
}

type req struct {
	method, path, body string
	host, remote       string
	hdr                map[string]string
}

func do(h http.Handler, q req) *httptest.ResponseRecorder {
	var r *http.Request
	if q.body != "" {
		r = httptest.NewRequest(q.method, q.path, strings.NewReader(q.body))
	} else {
		r = httptest.NewRequest(q.method, q.path, nil)
	}
	r.Host = "192.168.1.1:2017"
	if q.host != "" {
		r.Host = q.host
	}
	if q.remote != "" {
		r.RemoteAddr = q.remote
	}
	for k, v := range q.hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestUnauthenticated(t *testing.T) {
	h, _ := newTestServer(t)
	if w := do(h, req{method: "GET", path: "/api/status"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("без сессии: %d", w.Code)
	}
	if w := do(h, req{method: "GET", path: "/api/status", hdr: map[string]string{"Cookie": auth.CookieName + "=forged"}}); w.Code != http.StatusUnauthorized {
		t.Fatalf("поддельная сессия: %d", w.Code)
	}
}

func TestOriginMismatch(t *testing.T) {
	h, _ := newTestServer(t)
	w := do(h, req{method: "POST", path: "/api/nodes", body: "{}", hdr: map[string]string{"Origin": "http://evil.example"}})
	if w.Code != http.StatusForbidden {
		t.Fatalf("чужой Origin: %d", w.Code)
	}
}

func TestHostCheck(t *testing.T) {
	t.Setenv("NETPILOT_ALLOWED_HOSTS", "router.home, Proxy.Example.org ")
	h, _ := newTestServer(t)
	hn, _ := os.Hostname()
	short, _, _ := strings.Cut(hn, ".")
	for _, host := range []string{"evil.example", "evil.example:2017", "localhost.evil.example", "router.home.evil.example"} {
		for _, path := range []string{"/", "/api/auth/state"} {
			if w := do(h, req{method: "GET", path: path, host: host}); w.Code != http.StatusMisdirectedRequest {
				t.Errorf("Host %q %s: %d", host, path, w.Code)
			}
		}
	}
	allowed := []string{"192.168.1.1", "192.168.1.1:2017", "[::1]:2017", "[fe80::1]", "localhost:2017", "LOCALHOST",
		"ui.localhost", "router.home:2017", "proxy.example.org", "Router.Home.", "", short + ":2017", short + ".local", short + ".lan"}
	for _, host := range allowed {
		r := httptest.NewRequest("GET", "/", nil)
		r.Host = host
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("Host %q: %d", host, w.Code)
		}
	}

	t.Setenv("NETPILOT_ALLOWED_HOSTS", "*")
	h2, _ := newTestServer(t)
	if w := do(h2, req{method: "GET", path: "/", host: "anything.example"}); w.Code != http.StatusOK {
		t.Fatalf("NETPILOT_ALLOWED_HOSTS=*: %d", w.Code)
	}
}

func authState(t *testing.T, h http.Handler, remote string) map[string]bool {
	t.Helper()
	w := do(h, req{method: "GET", path: "/api/auth/state", remote: remote})
	var st map[string]bool
	if err := json.Unmarshal(w.Body.Bytes(), &st); err != nil {
		t.Fatalf("auth/state: %d %s", w.Code, w.Body)
	}
	return st
}

func TestSetupFromLoopback(t *testing.T) {
	h, au := newTestServer(t)
	if _, err := au.NewSetupCode(); err != nil {
		t.Fatal(err)
	}
	if st := authState(t, h, "127.0.0.1:5000"); !st["setup"] || st["setupCode"] {
		t.Fatalf("состояние с этого компьютера: %v", st)
	}
	w := do(h, req{method: "POST", path: "/api/auth/setup", body: `{"password":"correct horse"}`, remote: "[::1]:5000"})
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Set-Cookie"), auth.CookieName) {
		t.Fatalf("настройка с loopback: %d %s", w.Code, w.Body)
	}
	if !au.Configured() {
		t.Fatal("пароль не задан")
	}
}

func TestSetupRemoteNeedsCode(t *testing.T) {
	h, au := newTestServer(t)
	code, err := au.NewSetupCode()
	if err != nil {
		t.Fatal(err)
	}
	const remote = "192.168.1.50:40000"
	if st := authState(t, h, remote); !st["setup"] || !st["setupCode"] {
		t.Fatalf("состояние из сети: %v", st)
	}
	// X-Forwarded-For не делает запрос «локальным».
	w := do(h, req{method: "POST", path: "/api/auth/setup", body: `{"password":"correct horse"}`, remote: remote,
		hdr: map[string]string{"X-Forwarded-For": "127.0.0.1"}})
	if w.Code != http.StatusForbidden || au.Configured() {
		t.Fatalf("без кода: %d %s", w.Code, w.Body)
	}
	w = do(h, req{method: "POST", path: "/api/auth/setup", body: `{"password":"correct horse","code":"AAAA-BBBB-CCCC"}`, remote: remote})
	if w.Code != http.StatusForbidden || au.Configured() {
		t.Fatalf("неверный код: %d %s", w.Code, w.Body)
	}
	w = do(h, req{method: "POST", path: "/api/auth/setup", body: `{"password":"correct horse","code":"` + code + `"}`, remote: remote})
	if w.Code != http.StatusOK || !au.Configured() {
		t.Fatalf("верный код: %d %s", w.Code, w.Body)
	}
	if st := authState(t, h, remote); st["setup"] || st["setupCode"] {
		t.Fatalf("после настройки: %v", st)
	}
}
