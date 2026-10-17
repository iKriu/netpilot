package model

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// SchemaVersion — версия формата файла конфигурации. Увеличивается, когда
// поле переименовывается, меняет смысл или тип: простое добавление поля
// версии не требует (новые поля получают значения из Defaults).
const SchemaVersion = 1

// Migration переводит сырой JSON конфигурации с версии N на N+1.
// Работает с картой, а не с Config: переименованное поле в структуре уже
// не прочитать, а в сыром JSON оно ещё есть.
type Migration func(raw map[string]any) error

// migrations[N] переводит версию N в N+1; длина среза всегда равна SchemaVersion.
var migrations = []Migration{
	// 0 → 1: файлы до появления версии. Формат не менялся — только отметка версии.
	func(map[string]any) error { return nil },
}

// Migrate по очереди применяет миграции с версии from до SchemaVersion.
// Версию новее понятной программе не трогаем: старая программа потеряла бы
// поля, о которых не знает, и записала бы файл уже без них.
func Migrate(raw map[string]any, from int) error {
	return migrate(raw, from, migrations)
}

// MigrateJSON — Migrate для JSON-объекта конфигурации: разбирает, обновляет
// и собирает обратно. Если версия текущая, данные возвращаются как есть.
func MigrateJSON(data []byte, from int) ([]byte, error) {
	if from == SchemaVersion {
		return data, nil
	}
	var raw map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber() // большие числа (время, трафик) не теряют точность через float64
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	if raw == nil {
		raw = map[string]any{}
	}
	if err := Migrate(raw, from); err != nil {
		return nil, err
	}
	return json.Marshal(raw)
}

func migrate(raw map[string]any, from int, steps []Migration) error {
	if from > len(steps) {
		return fmt.Errorf("настройки сохранены более новой версией NetPilot (формат %d, эта версия понимает до %d) — обновите программу или восстановите резервную копию", from, len(steps))
	}
	if from < 0 {
		return fmt.Errorf("неверная версия формата настроек: %d", from)
	}
	for v := from; v < len(steps); v++ {
		if err := steps[v](raw); err != nil {
			return fmt.Errorf("обновление формата настроек %d → %d: %w", v, v+1, err)
		}
	}
	return nil
}
