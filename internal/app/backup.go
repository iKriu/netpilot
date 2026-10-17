package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"netpilot/internal/model"
)

// Backup — резервная копия настроек. Пароль администратора сюда не входит:
// файл копии не должен давать доступ к панели.
type Backup struct {
	Format  string `json:"format"`
	Version string `json:"version"`
	// Schema — версия формата Config: копия от старой версии пройдёт те же
	// миграции, что и файл настроек. Нет поля — копия до появления версий (0).
	Schema  int           `json:"schema"`
	Created time.Time     `json:"created"`
	Config  *model.Config `json:"config"`
}

const backupFormat = "netpilot-backup/1"

// restoreMaxErrors — сколько ошибок проверки показать: длинный список
// в уведомлении панели всё равно не прочитать.
const restoreMaxErrors = 5

func (a *App) Backup() Backup {
	return Backup{Format: backupFormat, Version: Version, Schema: model.SchemaVersion, Created: time.Now(), Config: a.Store.Get()}
}

// Restore проверяет копию и заменяет ею текущие настройки.
func (a *App) Restore(data []byte) error {
	cfg, err := parseBackup(data)
	if err != nil {
		return err
	}
	if err := a.Store.Replace(cfg); err != nil {
		return err
	}
	// DevicesChanged сам вызывает Changed — панель покажет «есть неприменённые
	// изменения»; кроме того, закрепления IP уходят в DHCP, маршруты — в перехват.
	a.DevicesChanged()
	return nil
}

// parseBackup разбирает и проверяет копию, не трогая текущие настройки.
func parseBackup(data []byte) (*model.Config, error) {
	// Config читается сырым: накладывать его на Defaults нужно из JSON,
	// иначе отсутствующие в старой копии поля станут нулями, а не значениями
	// по умолчанию.
	var b struct {
		Format string          `json:"format"`
		Schema int             `json:"schema"`
		Config json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("файл не похож на резервную копию: %w", err)
	}
	if b.Format != backupFormat || len(b.Config) == 0 || string(b.Config) == "null" {
		return nil, errors.New("файл не является резервной копией NetPilot")
	}
	raw, err := model.MigrateJSON(b.Config, b.Schema)
	if err != nil {
		return nil, fmt.Errorf("резервная копия: %w", err)
	}
	cfg := model.Defaults()
	if err := json.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("резервная копия повреждена: %w", err)
	}
	if errs := model.Validate(cfg); len(errs) > 0 {
		msgs := make([]string, 0, restoreMaxErrors+1)
		for i, e := range errs {
			if i == restoreMaxErrors {
				msgs = append(msgs, fmt.Sprintf("и ещё %d", len(errs)-restoreMaxErrors))
				break
			}
			msgs = append(msgs, e.Error())
		}
		return nil, errors.New("в резервной копии ошибки: " + strings.Join(msgs, "; "))
	}
	return cfg, nil
}
