package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"netpilot/internal/model"
)

// Backup — резервная копия настроек. Пароль администратора сюда не входит:
// файл копии не должен давать доступ к панели.
type Backup struct {
	Format  string        `json:"format"`
	Version string        `json:"version"`
	Created time.Time     `json:"created"`
	Config  *model.Config `json:"config"`
}

const backupFormat = "netpilot-backup/1"

func (a *App) Backup() Backup {
	return Backup{Format: backupFormat, Version: Version, Created: time.Now(), Config: a.Store.Get()}
}

// Restore проверяет копию и заменяет ею текущие настройки.
func (a *App) Restore(data []byte) error {
	var b Backup
	if err := json.Unmarshal(data, &b); err != nil {
		return fmt.Errorf("файл не похож на резервную копию: %w", err)
	}
	if b.Format != backupFormat || b.Config == nil {
		return errors.New("файл не является резервной копией NetPilot")
	}
	// Поверх значений по умолчанию: копия от старой версии получит новые поля.
	cfg := model.Defaults()
	raw, _ := json.Marshal(b.Config)
	if err := json.Unmarshal(raw, cfg); err != nil {
		return err
	}
	if cfg.Settings.MixedPort <= 0 || cfg.Settings.MixedPort > 65535 {
		return errors.New("в копии неверный порт прокси")
	}
	if err := a.Store.Replace(cfg); err != nil {
		return err
	}
	a.DevicesChanged() // закрепления IP — в DHCP, маршруты — в перехват
	return nil
}
