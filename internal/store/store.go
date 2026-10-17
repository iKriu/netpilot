// Package store хранит конфигурацию в одном JSON-файле.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"netpilot/internal/model"
)

// Store держит неизменяемые снимки конфигурации. Чтение — просто загрузка
// указателя без копирования: Get вызывается часто (опрос трафика раз в 2 секунды,
// каждый запрос панели), а с большими подписками копия через JSON на роутере
// заметно нагружает процессор и сборщик мусора. Копирование — только в Update.
type Store struct {
	mu   sync.Mutex // упорядочивает записи; чтение блокировок не берёт
	path string
	cur  atomic.Pointer[model.Config]
}

// onDisk — формат файла: поля конфигурации плюс версия формата рядом с ними.
// Встраивание указателя поднимает поля Config на верхний уровень JSON,
// поэтому model.Config ничего не знает о версии.
type onDisk struct {
	Schema int `json:"schema"`
	*model.Config
}

// Open загружает конфигурацию. firstRun вызывается, только если файла ещё нет:
// так окружение (например, Docker) задаёт свои значения по умолчанию.
func Open(dataDir string, firstRun func(*model.Config)) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: filepath.Join(dataDir, "config.json")}
	cfg := model.Defaults()
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		if firstRun != nil {
			firstRun(cfg)
		}
		if err := s.save(cfg); err != nil {
			return nil, err
		}
		s.cur.Store(cfg)
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	migrated, err := load(data, cfg)
	if err != nil {
		return nil, err
	}
	if migrated {
		// Прежний файл оставляем рядом: если миграция ошиблась, его можно вернуть.
		_ = os.WriteFile(s.path+".bak", data, 0o600)
		if err := s.save(cfg); err != nil {
			return nil, err
		}
	}
	s.cur.Store(cfg)
	return s, nil
}

// load разбирает файл поверх значений по умолчанию (cfg): новые поля настроек
// получат разумные значения. Старый формат сначала проходит миграции.
// migrated — формат обновлялся и файл стоит перезаписать.
func load(data []byte, cfg *model.Config) (migrated bool, err error) {
	var head struct {
		Schema int `json:"schema"` // нет поля — 0: файлы до появления версии
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return false, fmt.Errorf("файл настроек повреждён: %w", err)
	}
	if head.Schema != model.SchemaVersion {
		if data, err = model.MigrateJSON(data, head.Schema); err != nil {
			return false, err
		}
		migrated = true
	}
	if err := json.Unmarshal(data, &onDisk{Config: cfg}); err != nil {
		return false, fmt.Errorf("файл настроек повреждён: %w", err)
	}
	return migrated, nil
}

// Get возвращает текущий снимок конфигурации БЕЗ КОПИРОВАНИЯ.
//
// ВНИМАНИЕ: снимок общий для всех горутин — его НЕЛЬЗЯ изменять: ни поля,
// ни элементы срезов (cfg.Nodes[i].Name = ..., sort/slices.Sort по cfg.Rules,
// append к срезу из снимка с последующей записью). Изменение увидят все
// читатели, и это гонка данных. Менять конфигурацию — только через Update.
// Копия структуры по значению (s := cfg.Settings) безопасна, пока не трогаются
// срезы внутри неё; если нужен изменяемый срез — slices.Clone.
func (s *Store) Get() *model.Config {
	return s.cur.Load()
}

// Update изменяет конфигурацию и сохраняет её на диск. fn получает глубокую
// копию текущего снимка; после успешной записи копия становится новым снимком.
// Если fn вернул ошибку или запись не удалась, снимок остаётся прежним.
func (s *Store) Update(fn func(c *model.Config) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := clone(s.cur.Load())
	if err := fn(next); err != nil {
		return err
	}
	return s.commit(next)
}

// Replace заменяет конфигурацию целиком (восстановление из резервной копии).
// cfg копируется: вызывающий может продолжать им пользоваться.
func (s *Store) Replace(cfg *model.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commit(clone(cfg))
}

// commit сохраняет next и публикует его как текущий снимок. Вызывается под mu.
func (s *Store) commit(next *model.Config) error {
	if err := s.save(next); err != nil {
		return err
	}
	s.cur.Store(next)
	return nil
}

// save пишет во временный файл и переименовывает: при сбое питания
// старый конфиг не повредится.
func (s *Store) save(cfg *model.Config) error {
	data, err := json.MarshalIndent(onDisk{Schema: model.SchemaVersion, Config: cfg}, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// clone — глубокая копия через JSON. Медленнее ручного копирования, но не
// ошибается при добавлении в модель новых полей-срезов: копия никогда
// не делит память со снимком, который читают другие горутины.
func clone(c *model.Config) *model.Config {
	data, err := json.Marshal(c)
	if err != nil {
		panic("store: копирование конфигурации: " + err.Error()) // в модели только простые типы
	}
	out := &model.Config{}
	if err := json.Unmarshal(data, out); err != nil {
		panic("store: копирование конфигурации: " + err.Error())
	}
	return out
}

func NewID() string {
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
