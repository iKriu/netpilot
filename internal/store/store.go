// Package store хранит конфигурацию в одном JSON-файле.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"netpilot/internal/model"
)

type Store struct {
	mu   sync.RWMutex
	path string
	cfg  *model.Config
}

// Open загружает конфигурацию. firstRun вызывается, только если файла ещё нет:
// так окружение (например, Docker) задаёт свои значения по умолчанию.
func Open(dataDir string, firstRun func(*model.Config)) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dataDir, "config.json"), cfg: model.Defaults()}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		if firstRun != nil {
			firstRun(s.cfg)
		}
		return s, s.save()
	}
	if err != nil {
		return nil, err
	}
	// Поверх значений по умолчанию: новые поля настроек получат разумные значения.
	if err := json.Unmarshal(data, s.cfg); err != nil {
		return nil, err
	}
	return s, nil
}

// Get возвращает копию конфигурации.
func (s *Store) Get() *model.Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.cfg)
}

// Update изменяет конфигурацию под блокировкой и сохраняет её на диск.
// Если fn вернул ошибку, изменения отбрасываются.
func (s *Store) Update(fn func(c *model.Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.cfg)
	if err := fn(next); err != nil {
		return err
	}
	prev := s.cfg
	s.cfg = next
	if err := s.save(); err != nil {
		s.cfg = prev
		return err
	}
	return nil
}

// Replace заменяет конфигурацию целиком (восстановление из резервной копии).
func (s *Store) Replace(cfg *model.Config) error {
	return s.Update(func(c *model.Config) error {
		*c = *clone(cfg)
		return nil
	})
}

// save пишет во временный файл и переименовывает: при сбое питания
// старый конфиг не повредится.
func (s *Store) save() error {
	data, err := json.MarshalIndent(s.cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func clone(c *model.Config) *model.Config {
	data, _ := json.Marshal(c)
	out := &model.Config{}
	_ = json.Unmarshal(data, out)
	return out
}

func NewID() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
