package api

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestStaticServer(t *testing.T) {
	big := strings.Repeat("netpilot ", 2000)
	fsys := fstest.MapFS{
		"index.html":                   {Data: []byte("<!doctype html>" + big)},
		"m3/index.html":                {Data: []byte("m3")},
		"m3/54d9e68a5bb1127de3a8.wasm": {Data: []byte(big)},
	}
	s := newStaticServer(fsys)
	get := func(path string, hdr map[string]string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	w := get("/", map[string]string{"Accept-Encoding": "gzip"})
	if w.Code != 200 || w.Header().Get("Content-Encoding") != "gzip" || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("index: %d %v", w.Code, w.Header())
	}
	zr, _ := gzip.NewReader(w.Body)
	b, _ := io.ReadAll(zr)
	if !strings.HasPrefix(string(b), "<!doctype html>") {
		t.Fatal("gzip-содержимое не совпало")
	}
	etag := w.Header().Get("ETag")
	if w := get("/", map[string]string{"If-None-Match": etag}); w.Code != http.StatusNotModified {
		t.Fatalf("повторный запрос: %d", w.Code)
	}
	if w := get("/m3", nil); w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/m3/" {
		t.Fatalf("папка без слэша: %d %v", w.Code, w.Header())
	}
	if w := get("/m3/", nil); w.Body.String() != "m3" {
		t.Fatalf("/m3/: %q", w.Body.String())
	}
	w = get("/m3/54d9e68a5bb1127de3a8.wasm", nil)
	if w.Header().Get("Content-Type") != "application/wasm" || !strings.Contains(w.Header().Get("Cache-Control"), "immutable") {
		t.Fatalf("wasm: %v", w.Header())
	}
	if w := get("/nope.js", nil); w.Code != 404 {
		t.Fatalf("нет файла: %d", w.Code)
	}
	if w := get("/../secret", nil); w.Code != 404 {
		t.Fatalf("выход за пределы: %d", w.Code)
	}
}

// blockingFS «зависает» на чтении одного файла, пока не закрыт release.
type blockingFS struct {
	fstest.MapFS
	slow    string
	entered chan struct{}
	release chan struct{}
}

func (b blockingFS) ReadFile(name string) ([]byte, error) {
	if name == b.slow {
		close(b.entered)
		<-b.release
	}
	return b.MapFS.ReadFile(name)
}

// Пока готовится (сжимается) большой файл, остальные отдаются без ожидания.
func TestStaticServerPerFileLoading(t *testing.T) {
	fsys := blockingFS{
		MapFS:   fstest.MapFS{"big.wasm": {Data: []byte("wasm")}, "index.html": {Data: []byte("ui")}},
		slow:    "big.wasm",
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	s := newStaticServer(fsys)
	go s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/big.wasm", nil))
	<-fsys.entered
	done := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/index.html", nil))
		done <- w.Code
	}()
	select {
	case code := <-done:
		if code != 200 {
			t.Fatalf("index.html: %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("загрузка одного файла блокирует остальные")
	}
	close(fsys.release)
}
