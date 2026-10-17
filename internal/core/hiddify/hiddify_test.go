package hiddify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"netpilot/internal/core"
	"netpilot/internal/links"
	"netpilot/internal/model"
)

// Живая проверка: настоящий Hiddify Core принимает наш конфиг и отдаёт Clash API.
// Запуск: NETPILOT_HIDDIFY_DIR=<папка с распакованным ядром> go test ./internal/core/hiddify
func TestHiddifyRunsOurConfig(t *testing.T) {
	src := os.Getenv("NETPILOT_HIDDIFY_DIR")
	if src == "" {
		t.Skip("NETPILOT_HIDDIFY_DIR не задан")
	}
	nodes, errs := links.ParseMany(`
vless://b831381d-6324-4d53-ad4f-8cda48b30811@1.2.3.4:443?type=tcp&security=reality&sni=www.microsoft.com&fp=chrome&pbk=jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0&sid=ab12&flow=xtls-rprx-vision#DE-1
vless://b831381d-6324-4d53-ad4f-8cda48b30811@de2.example.com:443?type=ws&security=tls&path=%2Fws&host=cdn.example.com#DE-2
trojan://secret@tr.example.com:8443?type=grpc&serviceName=gun&sni=tr.example.com#NL-1
ss://YWVzLTI1Ni1nY206cGFzcw@5.6.7.8:8388#SS
hy2://pw@hy.example.com:443?sni=hy.example.com&obfs=salamander&obfs-password=x#HY
tuic://b831381d-6324-4d53-ad4f-8cda48b30811:pw@tu.example.com:443?congestion_control=bbr&alpn=h3#TU
`)
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	cfg := model.Defaults()
	for i := range nodes {
		nodes[i].ID = string(rune('a' + i))
	}
	cfg.Nodes = nodes
	cfg.Groups = []model.Group{{ID: "de", Name: "Германия", Type: "auto", Filter: "^DE"}, {ID: "m", Name: "Ручная", Type: "select", NodeIDs: []string{"c", "d"}}}
	cfg.Rules = []model.Rule{
		{ID: "1", Enabled: true, What: model.What{Apps: []string{"youtube", "telegram"}}, Where: model.Where{Action: "proxy", Target: "de"}},
		{ID: "2", Enabled: true, What: model.What{Protocols: []string{"bittorrent"}}, Where: model.Where{Action: "direct"}},
		{ID: "3", Enabled: true, What: model.What{Domains: []string{"full:a.example.com", "keyword:ads"}}, Where: model.Where{Action: "block"}},
	}
	cfg.Settings.DefaultTarget = "de"
	cfg.Settings.MixedPort = freePort(t)
	osName := "test"
	// Режим шлюза (TPROXY, обратный DNS): нужен Linux и право NET_ADMIN.
	if os.Getenv("NETPILOT_HIDDIFY_TPROXY") != "" {
		cfg.Settings.Mode, cfg.Settings.TProxyPort, osName = "transparent", freePort(t), "linux"
	}

	data := t.TempDir()
	c := New(data)
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{binary, "hiddify-core.dll", "libcronet.dll", "libcronet.so"} {
		if b, err := os.ReadFile(filepath.Join(src, f)); err == nil {
			_ = os.WriteFile(filepath.Join(c.dir, f), b, 0o755)
		}
	}
	_ = os.WriteFile(filepath.Join(c.dir, "VERSION"), []byte("test"), 0o644)
	clash := "127.0.0.1:" + itoa(freePort(t))
	if err := c.Start(cfg, core.Env{DataDir: data, OS: osName, ClashAddr: clash}); err != nil {
		log, _ := os.ReadFile(filepath.Join(data, "core.log"))
		t.Fatalf("Hiddify не запустился: %v\n%s", err, log)
	}
	defer c.Stop()
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", clash, time.Second)
		if err == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			log, _ := os.ReadFile(filepath.Join(data, "core.log"))
			t.Fatalf("Clash API не поднялся: %s", log)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !c.Running() {
		t.Fatal("ядро завершилось")
	}
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func itoa(n int) string {
	b := []byte{}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// Живая установка с GitHub: скачивание, сверка SHA-256, распаковка tar.gz.
// Запуск: NETPILOT_LIVE_INSTALL=1 go test -run Install ./internal/core/hiddify
func TestInstallFromGitHub(t *testing.T) {
	if os.Getenv("NETPILOT_LIVE_INSTALL") == "" {
		t.Skip("NETPILOT_LIVE_INSTALL не задан")
	}
	dl := func(ctx context.Context, url string, limit int64) ([]byte, error) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			return nil, fmt.Errorf("%s: %s", url, res.Status)
		}
		return io.ReadAll(io.LimitReader(res.Body, limit))
	}
	c := New(t.TempDir())
	v, err := c.Install(context.Background(), dl)
	if err != nil {
		t.Fatal(err)
	}
	if !c.Available() || c.Version() != v {
		t.Fatalf("после установки: доступно=%v версия=%q", c.Available(), c.Version())
	}
	out, err := exec.Command(filepath.Join(c.dir, binary), "version").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "hiddify-core") {
		t.Fatalf("ядро не запускается: %v %s", err, out)
	}
	t.Logf("установлено %s: %s", v, strings.SplitN(string(out), "\n", 2)[0])
}

// Hiddify собран на sing-box 1.13: поле, которого там нет, роняет запуск
// («unknown field»). Журнал — только с полями, известными 1.13.
func TestRenderLogCompatible113(t *testing.T) {
	data, err := New(t.TempDir()).Render(model.Defaults(), core.Env{OS: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	var conf struct {
		Log map[string]any `json:"log"`
	}
	if err := json.Unmarshal(data, &conf); err != nil {
		t.Fatal(err)
	}
	for k := range conf.Log {
		if k != "level" && k != "timestamp" && k != "output" && k != "disabled" {
			t.Errorf("поле log.%s неизвестно sing-box 1.13", k)
		}
	}
}
