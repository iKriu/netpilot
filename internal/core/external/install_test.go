package external

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGitHub — Downloader без сети: отдаёт описание релиза и файлы по адресам.
func fakeGitHub(t *testing.T, assets map[string][]byte, digests map[string]string) Downloader {
	t.Helper()
	type asset struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest,omitempty"`
	}
	rel := struct {
		Tag    string  `json:"tag_name"`
		Assets []asset `json:"assets"`
	}{Tag: "v1.2.3"}
	for name := range assets {
		rel.Assets = append(rel.Assets, asset{name, "https://dl.test/" + name, digests[name]})
	}
	relJSON, _ := json.Marshal(rel)
	return func(_ context.Context, url string, _ int64) ([]byte, error) {
		if strings.HasSuffix(url, "/releases/latest") {
			return relJSON, nil
		}
		if b, ok := assets[strings.TrimPrefix(url, "https://dl.test/")]; ok {
			return b, nil
		}
		return nil, fmt.Errorf("нет %s", url)
	}
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestInstallVerifiesChecksum(t *testing.T) {
	bin := []byte("core binary")
	other := []byte("something else")
	plain := Release{Repo: "x/core", Asset: func(string) string { return "core-bin" }, Binary: "core"}
	withDgst := plain
	withDgst.Digest = func(a string) string { return a + ".dgst" }

	cases := []struct {
		name    string
		rel     Release
		assets  map[string][]byte
		digests map[string]string
		wantErr string // "" — установка должна пройти
	}{
		{"github digest совпал", plain, map[string][]byte{"core-bin": bin},
			map[string]string{"core-bin": "sha256:" + strings.ToUpper(sha(bin))}, ""},
		{"github digest не совпал", plain, map[string][]byte{"core-bin": bin},
			map[string]string{"core-bin": "sha256:" + sha(other)}, "не совпала"},
		{"github digest нет", plain, map[string][]byte{"core-bin": bin}, nil, "не сообщил"},
		{"github digest другой алгоритм", plain, map[string][]byte{"core-bin": bin},
			map[string]string{"core-bin": "md5:abc"}, "не сообщил"},
		{"dgst совпал", withDgst, map[string][]byte{"core-bin": bin,
			"core-bin.dgst": []byte("MD5= x\nSHA2-256= " + sha(bin) + "\n")}, nil, ""},
		{"dgst не совпал", withDgst, map[string][]byte{"core-bin": bin,
			"core-bin.dgst": []byte("SHA2-256= " + sha(other) + "\n")}, nil, "не совпала"},
		{"dgst нет файла", withDgst, map[string][]byte{"core-bin": bin}, nil, "нет контрольной суммы"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			v, err := Install(context.Background(), fakeGitHub(t, tc.assets, tc.digests), tc.rel, dir)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("ошибка: %v", err)
				}
				if v != "1.2.3" {
					t.Fatalf("версия %q", v)
				}
				if b, _ := os.ReadFile(filepath.Join(dir, "core")); string(b) != string(bin) {
					t.Fatalf("файл ядра не записан")
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("ждали ошибку с %q, получили %v", tc.wantErr, err)
			}
			if _, err := os.Stat(filepath.Join(dir, "core")); err == nil {
				t.Fatalf("непроверенный файл ядра установлен")
			}
		})
	}
}
