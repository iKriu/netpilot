package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const fakeVersion = "9.9.9"

// TestMain: тестовый бинарник служит и «новой версией» в релизе — на пробный
// запуск с единственным аргументом -version он отвечает fakeVersion.
func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == "-version" {
		fmt.Println(fakeVersion)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type fakeRelease struct {
	sums, sig []byte // sig == nil — файла .sig в релизе нет
}

// fakeGitHub — API релизов и файлы одного релиза.
func fakeGitHub(t *testing.T, bin []byte, rel fakeRelease) {
	t.Helper()
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	files := map[string][]byte{assetName(): bin, sumsAsset: rel.sums}
	if rel.sig != nil {
		files[sigAsset] = rel.sig
	}
	mux.HandleFunc("GET /repos/test/netpilot/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		gr := map[string]any{"tag_name": "v" + fakeVersion, "html_url": srv.URL}
		var assets []map[string]any
		for name, b := range files {
			assets = append(assets, map[string]any{"name": name, "size": len(b), "url": srv.URL + "/api-asset/" + name, "browser_download_url": srv.URL + "/dl/" + name})
		}
		gr["assets"] = assets
		_ = json.NewEncoder(w).Encode(gr)
	})
	mux.HandleFunc("GET /dl/{name}", func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[r.PathValue("name")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	})
	old := apiBase
	apiBase = srv.URL
	t.Cleanup(func() { apiBase = old })
}

func setKey(t *testing.T, pub ed25519.PublicKey) {
	t.Helper()
	old := PublicKey
	PublicKey = ""
	if pub != nil {
		PublicKey = base64.StdEncoding.EncodeToString(pub)
	}
	t.Cleanup(func() { PublicKey = old })
}

func testBinary(t *testing.T) []byte {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sumsFor(bin []byte) []byte {
	h := sha256.Sum256(bin)
	return []byte(hex.EncodeToString(h[:]) + "  " + assetName() + "\n")
}

func sign(priv ed25519.PrivateKey, b []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, b)) + "\n")
}

// newTestUpdater — установленная «версия 1.0.0» во временной папке.
func newTestUpdater(t *testing.T) *Updater {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, "netpilot")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	u := &Updater{version: "1.0.0", dir: filepath.Join(dir, "update"), exe: exe, restart: make(chan struct{}, 1)}
	u.p.Repo = "test/netpilot"
	if err := os.MkdirAll(u.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return u
}

func installed(t *testing.T, u *Updater) bool {
	t.Helper()
	b, err := os.ReadFile(u.exe)
	if err != nil {
		t.Fatal(err)
	}
	return string(b) != "old"
}

func TestInstallSigned(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	setKey(t, pub)
	bin := testBinary(t)
	sums := sumsFor(bin)
	fakeGitHub(t, bin, fakeRelease{sums: sums, sig: sign(priv, sums)})
	u := newTestUpdater(t)
	if !u.State().Signed {
		t.Fatal("State.Signed должен быть true при встроенном ключе")
	}
	if err := u.Install(context.Background()); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if !installed(t, u) {
		t.Fatal("программа не заменена")
	}
	if err := u.Rollback(); err == nil || !strings.Contains(err.Error(), "установк") {
		t.Fatalf("откат во время установки должен отказывать, получено %v", err)
	}
}

func TestInstallBadSignature(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	_, other, _ := ed25519.GenerateKey(nil)
	setKey(t, pub)
	bin := testBinary(t)
	sums := sumsFor(bin)
	fakeGitHub(t, bin, fakeRelease{sums: sums, sig: sign(other, sums)})
	u := newTestUpdater(t)
	err := u.Install(context.Background())
	if err == nil || !strings.Contains(err.Error(), "подпись") {
		t.Fatalf("ожидалась ошибка подписи, получено %v", err)
	}
	if installed(t, u) {
		t.Fatal("программа заменена несмотря на чужую подпись")
	}
	if u.State().Installing {
		t.Fatal("флаг установки не сброшен")
	}
}

func TestInstallTamperedSums(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	setKey(t, pub)
	bin := testBinary(t)
	sums := sumsFor(bin)
	sig := sign(priv, sums)
	tampered := append([]byte("0000  other-file\n"), sums...)
	fakeGitHub(t, bin, fakeRelease{sums: tampered, sig: sig})
	u := newTestUpdater(t)
	if err := u.Install(context.Background()); err == nil || !strings.Contains(err.Error(), "подпись") {
		t.Fatalf("ожидалась ошибка подписи, получено %v", err)
	}
}

func TestInstallMissingSignature(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	setKey(t, pub)
	bin := testBinary(t)
	fakeGitHub(t, bin, fakeRelease{sums: sumsFor(bin)})
	u := newTestUpdater(t)
	if _, err := u.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "не подписан") {
		t.Fatalf("проверка должна сообщить о неподписанном релизе, получено %v", err)
	}
	if err := u.Install(context.Background()); err == nil || !strings.Contains(err.Error(), "не подписан") {
		t.Fatalf("ожидался отказ без %s, получено %v", sigAsset, err)
	}
	if installed(t, u) {
		t.Fatal("неподписанный релиз установлен")
	}
}

func TestInstallUnsignedBuild(t *testing.T) {
	setKey(t, nil)
	bin := testBinary(t)
	fakeGitHub(t, bin, fakeRelease{sums: sumsFor(bin)})
	u := newTestUpdater(t)
	if u.State().Signed {
		t.Fatal("State.Signed должен быть false без ключа")
	}
	if err := u.Install(context.Background()); err != nil {
		t.Fatalf("сборка без ключа ставит как раньше, по SHA256SUMS: %v", err)
	}
	if !installed(t, u) {
		t.Fatal("программа не заменена")
	}
}

func TestInstallUnsignedBuildChecksSum(t *testing.T) {
	setKey(t, nil)
	bin := testBinary(t)
	fakeGitHub(t, bin, fakeRelease{sums: sumsFor([]byte("другой файл"))})
	u := newTestUpdater(t)
	if err := u.Install(context.Background()); err == nil || !strings.Contains(err.Error(), "контрольная сумма") {
		t.Fatalf("ожидалась ошибка контрольной суммы, получено %v", err)
	}
}

func TestBrokenEmbeddedKey(t *testing.T) {
	old := PublicKey
	PublicKey = "не-base64"
	t.Cleanup(func() { PublicKey = old })
	if err := verifySums([]byte("x"), []byte("eA==")); err == nil || !strings.Contains(err.Error(), "ключ") {
		t.Fatalf("ожидалась ошибка ключа, получено %v", err)
	}
}

func TestChangesSource(t *testing.T) {
	u := newTestUpdater(t)
	u.p.Repo, u.p.Token = "", "tok"
	empty, tok, other := "", "tok", "new"
	for _, c := range []struct {
		repo  string
		token *string
		want  bool
	}{
		{DefaultRepo, nil, false}, {"", nil, false}, {"https://github.com/" + DefaultRepo + "/", nil, false},
		{DefaultRepo, &tok, false}, {"evil/netpilot", nil, true}, {DefaultRepo, &other, true}, {DefaultRepo, &empty, true},
	} {
		if got := u.ChangesSource(c.repo, c.token); got != c.want {
			t.Errorf("ChangesSource(%q, %v) = %v", c.repo, c.token, got)
		}
	}
}
