package lists

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestMihomoText(t *testing.T) {
	got := mihomoText("domain", []byte("# comment\nexample.com\n\n.x.org\n+.y.net\n"))
	if got != "+.example.com\n+.x.org\n+.y.net\n" {
		t.Errorf("домены: %q", got)
	}
	if got := mihomoText("ip", []byte("1.2.3.0/24\n")); got != "1.2.3.0/24\n" {
		t.Errorf("IP: %q", got)
	}
}

func TestUpdate(t *testing.T) {
	dir := t.TempDir()
	l, _ := Get("refilter-domains")
	calls := 0
	dl := func(_ context.Context, url string, _ int64) ([]byte, error) {
		calls++
		if url == l.SRS {
			return []byte("SRS\x01data"), nil
		}
		return []byte("example.com\n"), nil
	}
	changed, err := Update(context.Background(), dl, dir, l, time.Hour)
	if err != nil || !changed || !Ready(SRSPath(dir, l.ID)) {
		t.Fatalf("первая загрузка: %v %v", changed, err)
	}
	// Свежий список не перекачивается.
	if changed, _ := Update(context.Background(), dl, dir, l, time.Hour); changed || calls != 2 {
		t.Errorf("свежий список скачан повторно: calls=%d", calls)
	}
	// Мусор вместо набора правил не должен затереть рабочий файл.
	bad := func(context.Context, string, int64) ([]byte, error) { return []byte("<html>"), nil }
	if _, err := Update(context.Background(), bad, dir, l, 0); err == nil {
		t.Error("ожидалась ошибка на неверном файле")
	}
	if b, _ := os.ReadFile(SRSPath(dir, l.ID)); string(b) != "SRS\x01data" {
		t.Error("рабочий файл испорчен")
	}
	fail := func(context.Context, string, int64) ([]byte, error) { return nil, errors.New("нет сети") }
	if _, err := Update(context.Background(), fail, dir, l, 0); err == nil {
		t.Error("ожидалась ошибка сети")
	}
}
