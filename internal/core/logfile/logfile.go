// Package logfile — журналы ядер с ограничением размера. На роутерах журнал
// лежит во флеш-памяти: без ограничения он растёт, пока ядро работает, и
// однажды занимает всё место.
package logfile

import (
	"io"
	"os"
	"sync"
)

// MaxSize — после какого размера журнал переносится в «.1» (одна старая копия).
const MaxSize = 4 << 20

// Writer пишет в файл и, когда он дорастает до max, переименовывает его в
// path+".1" и начинает новый. Безопасен для одновременной записи: у процесса
// ядра stdout и stderr пишутся из разных горутин.
type Writer struct {
	mu   sync.Mutex
	path string
	max  int64
	f    *os.File
	size int64
}

// Create создаёт журнал заново (прежнее содержимое не нужно — это прошлый запуск).
func Create(path string, max int64) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	return &Writer{path: path, max: max, f: f}, nil
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return 0, os.ErrClosed
	}
	if w.size > 0 && w.size+int64(len(p)) > w.max {
		w.rotate()
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

// rotate: закрываем файл до переименования — на Windows открытый файл не переименовать.
func (w *Writer) rotate() {
	w.f.Close()
	_ = os.Remove(w.path + ".1")
	_ = os.Rename(w.path, w.path+".1")
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		// Не вышло — продолжаем в старый файл, лишь бы не потерять журнал.
		f, err = os.OpenFile(w.path+".1", os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			w.f = nil
			return
		}
	}
	w.f, w.size = f, 0
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

// Trim ограничивает журнал, который пишет кто-то другой (встроенный sing-box
// сам открывает файл с O_APPEND): содержимое копируется в path+".1", а файл
// обрезается до нуля. Запись дописывается в конец, поэтому после обрезки
// продолжается с начала файла. Строки, записанные между копированием и
// обрезкой, теряются — для журнала это допустимо.
func Trim(path string, max int64) error {
	st, err := os.Stat(path)
	if err != nil || st.Size() <= max {
		return nil
	}
	src, err := os.Open(path)
	if err != nil {
		return err
	}
	dst, err := os.Create(path + ".1")
	if err != nil {
		src.Close()
		return err
	}
	_, err = io.Copy(dst, src)
	src.Close()
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Truncate(path, 0)
}
