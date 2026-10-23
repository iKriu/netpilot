// Package store хранит конфигурацию в одном JSON-файле.
package store

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"netpilot/internal/atomicfile"
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
	// recovered — сообщение о восстановлении из резервной копии при запуске
	// (задаётся в Open и дальше не меняется, поэтому без блокировки).
	recovered string
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
	switch {
	case err != nil && corrupt(data):
		if cfg, err = s.restoreBackup(data, err); err != nil {
			return nil, err
		}
	case err != nil:
		// Файл цел, но не читается (например, формат новее программы) —
		// это не сбой диска, восстанавливать из копий нельзя: потеряли бы
		// настройки новой версии.
		return nil, err
	case migrated:
		// Прежний файл оставляем рядом: если миграция ошиблась, его можно вернуть.
		_ = atomicfile.Write(s.path+".bak", data, 0o600)
		if err := s.save(cfg); err != nil {
			return nil, err
		}
	}
	s.cur.Store(cfg)
	// Свежая точка восстановления — раз за запуск, а не при каждом сохранении:
	// на роутерах с флеш-памятью лишние записи изнашивают её. Копия новее
	// .bak (тот пишется только при миграции) и пригодится, если config.json
	// испортится при следующем отключении питания.
	if b, err := encode(cfg); err == nil {
		if err := atomicfile.Write(s.path+".prev", b, 0o600); err != nil {
			log.Printf("не удалось сохранить резервную копию настроек: %v", err)
		}
	}
	return s, nil
}

// corrupt — файл испорчен на уровне диска: пустой (обычный итог отключения
// питания на XFS/ext4) или вовсе не JSON (нули, обрывок). Файл с правильным
// JSON, который не подошёл по смыслу, повреждённым не считается.
func corrupt(data []byte) bool {
	return len(bytes.TrimSpace(data)) == 0 || !json.Valid(data)
}

// restoreBackup поднимает настройки из резервной копии, когда config.json испорчен.
// Раньше программа в этом случае завершалась, systemd перезапускал её сотни
// раз, и вся сеть оставалась без интернета, пока человек не вернул копию вручную.
//
// Берётся самая свежая из читаемых копий: .prev (сохраняется при каждом
// запуске) обычно новее .bak (остаётся от миграции формата). Испорченный файл
// не удаляется, а откладывается в config.json.broken-<время> — и только если
// копия нашлась: иначе следующий запуск не нашёл бы config.json и молча начал
// бы с настроек по умолчанию. Если годной копии нет, остаётся прежнее
// поведение — ошибка запуска.
func (s *Store) restoreBackup(data []byte, loadErr error) (*model.Config, error) {
	log.Printf("ВНИМАНИЕ: %s повреждён (%d байт, %v) — возможно, было внезапное отключение питания; пробую резервные копии", s.path, len(data), loadErr)
	type candidate struct {
		path string
		mod  time.Time
		cfg  *model.Config
	}
	var best *candidate
	for _, p := range []string{s.path + ".prev", s.path + ".bak"} {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		b, err := os.ReadFile(p)
		if err != nil {
			log.Printf("резервная копия %s не читается: %v", p, err)
			continue
		}
		c := model.Defaults()
		if _, err := load(b, c); err != nil {
			log.Printf("резервная копия %s не подходит: %v", p, err)
			continue
		}
		if best == nil || fi.ModTime().After(best.mod) {
			best = &candidate{path: p, mod: fi.ModTime(), cfg: c}
		}
	}
	if best == nil {
		return nil, fmt.Errorf("%w; годной резервной копии (config.json.prev, config.json.bak) нет — восстановите настройки вручную", loadErr)
	}
	broken := fmt.Sprintf("%s.broken-%d", s.path, time.Now().Unix())
	if err := os.Rename(s.path, broken); err != nil {
		return nil, fmt.Errorf("%w; отложить повреждённый файл не удалось: %v", loadErr, err)
	}
	atomicfile.SyncDir(filepath.Dir(s.path))
	if err := s.save(best.cfg); err != nil {
		return nil, err
	}
	log.Printf("ВНИМАНИЕ: настройки восстановлены из %s от %s; повреждённый файл сохранён как %s",
		filepath.Base(best.path), best.mod.Format("02.01.2006 15:04"), filepath.Base(broken))
	s.recovered = fmt.Sprintf("Настройки были повреждены (внезапное отключение питания?) и восстановлены из резервной копии от %s; изменения после неё нужно повторить.",
		best.mod.Format("02.01.2006 15:04"))
	return best.cfg, nil
}

// Recovered — сообщение для интерфейса, если при запуске config.json был
// повреждён и настройки подняты из резервной копии; иначе пустая строка.
func (s *Store) Recovered() string {
	return s.recovered
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

// save пишет атомарно и со сбросом на диск: при сбое питания на месте
// config.json останется прежняя или новая версия, но не пустой файл.
func (s *Store) save(cfg *model.Config) error {
	data, err := encode(cfg)
	if err != nil {
		return err
	}
	return atomicfile.Write(s.path, data, 0o600)
}

func encode(cfg *model.Config) ([]byte, error) {
	return json.MarshalIndent(onDisk{Schema: model.SchemaVersion, Config: cfg}, "", "  ")
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
