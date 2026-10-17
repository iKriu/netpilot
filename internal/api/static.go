package api

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
)

// staticServer отдаёт веб-интерфейс: с ETag (браузер не качает то же самое
// заново), со сжатием gzip (интерфейс Material 3 весит ~16 МБ, сжатый ~5 МБ)
// и с вечным кэшем для файлов, в имени которых хэш содержимого.
type staticServer struct {
	fsys  fs.FS
	mu    sync.Mutex
	cache map[string]*staticFile
}

type staticFile struct {
	mod   time.Time
	size  int64
	etag  string
	ctype string
	raw   []byte
	gz    []byte // nil — сжимать не стоит
}

// Файлы сборки Compose с хэшем в имени: 54d9e68a5bb1127de3a8.wasm.
var hashedAsset = regexp.MustCompile(`/[0-9a-f]{16,}\.(wasm|js)$`)

var compressible = map[string]bool{".wasm": true, ".js": true, ".css": true, ".html": true, ".json": true, ".svg": true, ".ttf": true, ".txt": true, ".map": true}

func newStaticServer(fsys fs.FS) *staticServer {
	return &staticServer{fsys: fsys, cache: map[string]*staticFile{}}
}

func (s *staticServer) load(name string) (*staticFile, error) {
	st, err := fs.Stat(s.fsys, name)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return nil, fs.ErrNotExist
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.cache[name]; ok && f.mod.Equal(st.ModTime()) && f.size == st.Size() {
		return f, nil
	}
	raw, err := fs.ReadFile(s.fsys, name)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	ext := strings.ToLower(path.Ext(name))
	ctype := mime.TypeByExtension(ext)
	switch ext {
	case ".wasm":
		ctype = "application/wasm"
	case ".ttf":
		ctype = "font/ttf"
	case ".js":
		ctype = "text/javascript; charset=utf-8"
	}
	if ctype == "" {
		ctype = http.DetectContentType(raw)
	}
	f := &staticFile{mod: st.ModTime(), size: st.Size(), etag: `"` + hex.EncodeToString(sum[:12]) + `"`, ctype: ctype, raw: raw}
	if compressible[ext] && len(raw) > 1024 {
		var b bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&b, gzip.BestCompression)
		_, _ = zw.Write(raw)
		_ = zw.Close()
		if b.Len() < len(raw)*9/10 {
			f.gz = b.Bytes()
		}
	}
	s.cache[name] = f
	return f, nil
}

func (s *staticServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p := r.URL.Path
	if strings.HasSuffix(p, "/") {
		p += "index.html"
	}
	name := strings.TrimPrefix(path.Clean(p), "/")
	if name == "" || name == "." {
		name = "index.html"
	}
	f, err := s.load(name)
	if err != nil {
		// Адрес папки без «/» в конце: /m3 → /m3/.
		if st, e := fs.Stat(s.fsys, name); e == nil && st.IsDir() {
			http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
			return
		}
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("ETag", f.etag)
	h.Set("Vary", "Accept-Encoding")
	if hashedAsset.MatchString("/" + name) {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		// После обновления программы браузер перепроверит файл по ETag.
		h.Set("Cache-Control", "no-cache")
	}
	if inm := r.Header.Get("If-None-Match"); inm != "" && strings.Contains(inm, f.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", f.ctype)
	body := f.raw
	if f.gz != nil && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		h.Set("Content-Encoding", "gzip")
		body = f.gz
	}
	h.Set("Content-Length", itoa(len(body)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.Copy(w, bytes.NewReader(body))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
