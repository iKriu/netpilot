// Package lists — внешние списки адресов (Re:filter и др.). NetPilot скачивает
// их сам и отдаёт ядру локальными файлами: запуск ядра не зависит от того,
// доступен ли сейчас сайт со списками.
package lists

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type List struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"` // domain | ip
	SRS         string `json:"-"`    // набор правил sing-box
	Text        string `json:"-"`    // текстовый список (для mihomo)
}

const refilter = "https://github.com/1andrevich/Re-filter-lists/releases/latest/download/"

var catalog = []List{
	{
		ID: "refilter-domains", Kind: "domain",
		Name:        "Re:filter — заблокированные сайты",
		Description: "Домены, заблокированные в России (реестр РКН и др.), обновляется ежедневно",
		SRS:         refilter + "ruleset-domain-refilter_domains.srs",
		Text:        refilter + "domains_all.lst",
	},
	{
		ID: "refilter-ips", Kind: "ip",
		Name:        "Re:filter — заблокированные IP",
		Description: "Адреса и подсети, заблокированные в России",
		SRS:         refilter + "ruleset-ip-refilter_ipsum.srs",
		Text:        refilter + "ipsum.lst",
	},
}

func All() []List { return catalog }

func Get(id string) (List, bool) {
	for _, l := range catalog {
		if l.ID == id {
			return l, true
		}
	}
	return List{}, false
}

// SRSPath и TextPath — где лежат скачанные файлы.
func SRSPath(dir, id string) string  { return filepath.Join(dir, id+".srs") }
func TextPath(dir, id string) string { return filepath.Join(dir, id+".txt") }

// Ready — скачан ли список (для ядра, которому нужен файл данного вида).
func Ready(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Size() > 0
}

type Status struct {
	List
	Ready   bool   `json:"ready"`
	Updated int64  `json:"updated,omitempty"`
	Size    int64  `json:"size,omitempty"`
	Entries int    `json:"entries,omitempty"`
	Error   string `json:"error,omitempty"`
}

func StatusOf(dir string, l List) Status {
	s := Status{List: l}
	if st, err := os.Stat(SRSPath(dir, l.ID)); err == nil {
		s.Ready, s.Updated, s.Size = true, st.ModTime().Unix(), st.Size()
	}
	if b, err := os.ReadFile(TextPath(dir, l.ID)); err == nil {
		s.Entries = bytes.Count(b, []byte("\n"))
	}
	return s
}

// Downloader — функция загрузки приложения (напрямую, при неудаче — через прокси).
type Downloader func(ctx context.Context, url string, limit int64) ([]byte, error)

// Update скачивает список, если его нет или он старше maxAge (0 — всегда).
// Возвращает true, если файлы изменились.
func Update(ctx context.Context, dl Downloader, dir string, l List, maxAge time.Duration) (bool, error) {
	if maxAge > 0 {
		if st, err := os.Stat(SRSPath(dir, l.ID)); err == nil && time.Since(st.ModTime()) < maxAge {
			return false, nil
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	srs, err := dl(ctx, l.SRS, 64<<20)
	if err != nil {
		return false, fmt.Errorf("%s: %w", l.Name, err)
	}
	if len(srs) < 4 || string(srs[:3]) != "SRS" {
		return false, fmt.Errorf("%s: файл не похож на набор правил sing-box", l.Name)
	}
	text, err := dl(ctx, l.Text, 64<<20)
	if err != nil {
		return false, fmt.Errorf("%s: %w", l.Name, err)
	}
	old, _ := os.ReadFile(SRSPath(dir, l.ID))
	if err := writeAtomic(SRSPath(dir, l.ID), srs); err != nil {
		return false, err
	}
	if err := writeAtomic(TextPath(dir, l.ID), []byte(mihomoText(l.Kind, text))); err != nil {
		return false, err
	}
	return !bytes.Equal(old, srs), nil
}

// mihomoText приводит список к формату правил mihomo: домен вместе с поддоменами
// записывается как «+.example.com», подсети — как есть.
func mihomoText(kind string, data []byte) string {
	var b strings.Builder
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if kind == "domain" {
			line = "+." + strings.TrimPrefix(strings.TrimPrefix(line, "+."), ".")
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
