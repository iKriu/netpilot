package model

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestMigrationsCoverEveryVersion(t *testing.T) {
	if len(migrations) != SchemaVersion {
		t.Fatalf("миграций %d, а версия формата %d: каждая версия N→N+1 нужна по порядку", len(migrations), SchemaVersion)
	}
}

func TestMigrateOrder(t *testing.T) {
	var trace []int
	step := func(v int) Migration {
		return func(raw map[string]any) error {
			trace = append(trace, v)
			raw["v"] = v + 1
			return nil
		}
	}
	steps := []Migration{step(0), step(1), step(2)}
	raw := map[string]any{}
	if err := migrate(raw, 1, steps); err != nil {
		t.Fatal(err)
	}
	if len(trace) != 2 || trace[0] != 1 || trace[1] != 2 || raw["v"] != 3 {
		t.Fatalf("порядок миграций: %v, v=%v", trace, raw["v"])
	}

	trace = nil
	if err := migrate(map[string]any{}, 3, steps); err != nil || len(trace) != 0 {
		t.Fatalf("текущая версия: %v %v", err, trace)
	}
	if err := migrate(map[string]any{}, 4, steps); err == nil || !strings.Contains(err.Error(), "более новой версией") {
		t.Fatalf("версия новее программы: %v", err)
	}
	if err := migrate(map[string]any{}, -1, steps); err == nil {
		t.Fatal("отрицательная версия принята")
	}
	bad := []Migration{func(map[string]any) error { return errors.New("сломано") }}
	if err := migrate(map[string]any{}, 0, bad); err == nil || !strings.Contains(err.Error(), "0 → 1") {
		t.Fatalf("ошибка шага: %v", err)
	}
}

func TestMigrateJSON(t *testing.T) {
	in := []byte(`{"settings":{"mixedPort":7890},"devices":[{"id":"d","firstSeen":9007199254740993}]}`)
	out, err := MigrateJSON(in, 0)
	if err != nil {
		t.Fatal(err)
	}
	var c Config
	if err := json.Unmarshal(out, &c); err != nil {
		t.Fatal(err)
	}
	// Число больше 2^53 не должно округлиться через float64.
	if c.Settings.MixedPort != 7890 || c.Devices[0].FirstSeen != 9007199254740993 {
		t.Fatalf("данные изменились: %s", out)
	}
	if same, _ := MigrateJSON(in, SchemaVersion); string(same) != string(in) {
		t.Fatal("текущая версия должна возвращаться без изменений")
	}
	if _, err := MigrateJSON(in, SchemaVersion+1); err == nil {
		t.Fatal("версия новее программы принята")
	}
}

// 1 → 2: прежний DNS по умолчанию становится auto, свой — остаётся.
func TestMigrateDNSAuto(t *testing.T) {
	for in, want := range map[string]string{"https://77.88.8.8/dns-query": DNSAuto, "tls://dns.google": "tls://dns.google"} {
		raw := map[string]any{"settings": map[string]any{"dnsDirect": in}}
		if err := Migrate(raw, 1); err != nil {
			t.Fatal(err)
		}
		if got := raw["settings"].(map[string]any)["dnsDirect"]; got != want {
			t.Errorf("%s → %v, ожидалось %s", in, got, want)
		}
	}
}
