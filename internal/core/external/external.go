// Package external — общее для ядер, которые работают отдельным процессом
// (Xray, mihomo): запуск с журналом, остановка, установка с GitHub.
package external

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Downloader скачивает файл (приложение подставляет свою функцию: напрямую,
// а при неудаче — через уже работающий прокси).
type Downloader func(ctx context.Context, url string, limit int64) ([]byte, error)

// Process — ядро, запущенное отдельным процессом.
type Process struct {
	mu   sync.Mutex
	cmd  *exec.Cmd
	done chan struct{}
	err  error
}

// Start запускает процесс и ждёт немного: если ядро сразу упало (ошибка в
// конфиге, занят порт), это нужно показать пользователю, а не считать запуском.
func (p *Process) Start(bin string, args []string, dir, logPath string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil {
		return errors.New("ядро уже запущено")
	}
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = logf, logf
	hideWindow(cmd)
	started := make(chan error, 1)
	done := make(chan struct{})
	go func() {
		// На Linux сигнал о смерти родителя (Pdeathsig) привязан к потоку,
		// который запустил процесс. Держим этот поток, пока ядро работает,
		// иначе Go мог бы его завершить — и ядро убилось бы посреди работы.
		runtime.LockOSThread()
		if err := cmd.Start(); err != nil {
			started <- err
			return
		}
		bindToParent(cmd) // Windows: ядро завершится вместе с NetPilot
		started <- nil
		err := cmd.Wait()
		logf.Close()
		p.mu.Lock()
		p.err = err
		p.mu.Unlock()
		close(done)
	}()
	if err := <-started; err != nil {
		logf.Close()
		return err
	}
	select {
	case <-done:
		return fmt.Errorf("ядро завершилось сразу после запуска: %s", tail(logPath, 600))
	case <-time.After(1200 * time.Millisecond):
	}
	p.cmd, p.done = cmd, done
	return nil
}

func (p *Process) Stop() error {
	p.mu.Lock()
	cmd, done := p.cmd, p.done
	p.cmd, p.done = nil, nil
	p.mu.Unlock()
	if cmd == nil {
		return nil
	}
	_ = cmd.Process.Kill()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		return errors.New("ядро не остановилось за 5 секунд")
	}
	return nil
}

// Signal отправляет сигнал работающему процессу (например, SIGHUP — перечитать настройки).
func (p *Process) Signal(sig os.Signal) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil || p.cmd.Process == nil {
		return errors.New("процесс не запущен")
	}
	return p.cmd.Process.Signal(sig)
}

func (p *Process) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil {
		return false
	}
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

// Test выполняет проверку конфига самим ядром (xray -test, mihomo -t).
func Test(bin string, args []string, dir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ядро отвергло конфиг: %s", lastLines(string(out), 6))
	}
	return nil
}

func tail(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return strings.TrimSpace(string(b))
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// ---------- установка ----------

// Release — описание, как скачать ядро для текущей платформы.
type Release struct {
	Repo   string                    // "XTLS/Xray-core"
	Asset  func(tag string) string   // имя файла релиза для этой ОС и архитектуры
	Digest func(asset string) string // имя файла с контрольной суммой ("" — нет)
	Binary string                    // имя исполняемого файла после распаковки
	// InArchive — исполняемый ли это файл ядра внутри zip (имена у ядер разные).
	InArchive func(base string) bool
	Extract   []string // что ещё достать из архива (geoip.dat, ...)
}

type ghRelease struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest"` // "sha256:…" — GitHub считает сам для каждого файла
	} `json:"assets"`
}

// Install скачивает последний релиз в dir и возвращает его версию.
func Install(ctx context.Context, dl Downloader, rel Release, dir string) (string, error) {
	raw, err := dl(ctx, "https://api.github.com/repos/"+rel.Repo+"/releases/latest", 1<<20)
	if err != nil {
		return "", fmt.Errorf("список релизов: %w", err)
	}
	var gh ghRelease
	if err := json.Unmarshal(raw, &gh); err != nil {
		return "", fmt.Errorf("список релизов: %w", err)
	}
	name := rel.Asset(gh.Tag)
	if name == "" {
		return "", fmt.Errorf("нет сборки для %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	urls, digests := map[string]string{}, map[string]string{}
	for _, a := range gh.Assets {
		urls[a.Name] = a.URL
		digests[a.Name] = a.Digest
	}
	if urls[name] == "" {
		return "", fmt.Errorf("в релизе %s нет файла %s", gh.Tag, name)
	}
	data, err := dl(ctx, urls[name], 160<<20)
	if err != nil {
		return "", fmt.Errorf("загрузка %s: %w", name, err)
	}
	if rel.Digest != nil {
		dname := rel.Digest(name)
		if urls[dname] == "" {
			return "", fmt.Errorf("в релизе нет контрольной суммы %s", dname)
		}
		dg, err := dl(ctx, urls[dname], 64<<10)
		if err != nil {
			return "", fmt.Errorf("контрольная сумма: %w", err)
		}
		sum := sha256.Sum256(data)
		if !strings.Contains(strings.ToLower(string(dg)), hex.EncodeToString(sum[:])) {
			return "", errors.New("контрольная сумма не совпала — файл повреждён или подменён")
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	files := map[string][]byte{}
	switch {
	case strings.HasSuffix(name, ".zip"):
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return "", err
		}
		for _, f := range zr.File {
			base := filepath.Base(f.Name)
			isBin := base == rel.Binary || (rel.InArchive != nil && rel.InArchive(base))
			if !isBin && !contains(rel.Extract, base) {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			b, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return "", err
			}
			if isBin {
				base = rel.Binary
			}
			files[base] = b
		}
	case strings.HasSuffix(name, ".tar.gz"):
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return "", err
		}
		tr := tar.NewReader(zr)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", err
			}
			if h.Typeflag != tar.TypeReg {
				continue
			}
			base := filepath.Base(h.Name)
			isBin := base == rel.Binary || (rel.InArchive != nil && rel.InArchive(base))
			if !isBin && !contains(rel.Extract, base) {
				continue
			}
			b, err := io.ReadAll(io.LimitReader(tr, 256<<20))
			if err != nil {
				return "", err
			}
			if isBin {
				base = rel.Binary
			}
			files[base] = b
		}
	case strings.HasSuffix(name, ".gz"):
		zr, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return "", err
		}
		b, err := io.ReadAll(zr)
		if err != nil {
			return "", err
		}
		files[rel.Binary] = b
	default:
		files[rel.Binary] = data
	}
	if files[rel.Binary] == nil {
		return "", fmt.Errorf("в архиве нет %s", rel.Binary)
	}
	// Пишем во временные файлы и переименовываем: работающее ядро не повредится.
	for fname, b := range files {
		tmp := filepath.Join(dir, fname+".new")
		if err := os.WriteFile(tmp, b, 0o755); err != nil {
			return "", err
		}
		if err := os.Rename(tmp, filepath.Join(dir, fname)); err != nil {
			return "", err
		}
	}
	version := strings.TrimPrefix(gh.Tag, "v")
	_ = os.WriteFile(filepath.Join(dir, "VERSION"), []byte(version), 0o644)
	return version, nil
}

// InstalledVersion читает версию установленного ядра ("" — не установлено).
func InstalledVersion(dir, binary string) string {
	if _, err := os.Stat(filepath.Join(dir, binary)); err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dir, "VERSION"))
	if err != nil {
		return "?"
	}
	return strings.TrimSpace(string(b))
}

func Uninstall(dir string) error { return os.RemoveAll(dir) }

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Exe добавляет .exe на Windows.
func Exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
