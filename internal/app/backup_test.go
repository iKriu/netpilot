package app

import (
	"encoding/json"
	"strings"
	"testing"

	"netpilot/internal/model"
	"netpilot/internal/store"
)

func TestParseBackup(t *testing.T) {
	// Копия от старой версии: без schema и без части настроек — недостающее
	// берётся из значений по умолчанию, а не обнуляется.
	old := `{"format":"netpilot-backup/1","version":"0.9","config":{
		"settings":{"mixedPort":8080,"mode":"proxy"},
		"nodes":[{"id":"n","name":"A","protocol":"vless","server":"a.example","port":443}]}}`
	cfg, err := parseBackup([]byte(old))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Settings.MixedPort != 8080 || cfg.Settings.TProxyPort != 7893 || cfg.Settings.DNSRemote == "" || len(cfg.Nodes) != 1 {
		t.Fatalf("наложение на значения по умолчанию: %+v", cfg.Settings)
	}

	cases := []struct {
		name, data, want string
	}{
		{"не json", `{`, "не похож"},
		{"чужой формат", `{"format":"x","config":{}}`, "не является"},
		{"без config", `{"format":"netpilot-backup/1"}`, "не является"},
		{"новее программы", `{"format":"netpilot-backup/1","schema":99,"config":{}}`, "более новой версией"},
		{"неверный порт", `{"format":"netpilot-backup/1","config":{"settings":{"mixedPort":70000}}}`, "порт прокси"},
	}
	for _, c := range cases {
		if _, err := parseBackup([]byte(c.data)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: ошибка %v, ожидалось «%s»", c.name, err, c.want)
		}
	}

	// Много ошибок — показываются первые пять и сколько ещё.
	bad := model.Defaults()
	for i := range 8 {
		bad.Devices = append(bad.Devices, model.Device{ID: string(rune('a' + i)), Name: "D", IP: "bad"})
	}
	data, _ := json.Marshal(Backup{Format: backupFormat, Config: bad})
	_, err = parseBackup(data)
	if err == nil || strings.Count(err.Error(), "не IPv4") != restoreMaxErrors || !strings.Contains(err.Error(), "и ещё 3") {
		t.Fatalf("список ошибок: %v", err)
	}
}

func TestBackupRestoreRoundTrip(t *testing.T) {
	st, err := store.Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{Store: st, DataDir: t.TempDir()}
	_ = st.Update(func(c *model.Config) error {
		c.Groups = []model.Group{{ID: "g", Name: "Европа", Filter: "DE"}}
		return nil
	})
	b := a.Backup()
	if b.Schema != model.SchemaVersion {
		t.Fatalf("schema в копии: %d", b.Schema)
	}
	data, _ := json.Marshal(b)
	cfg, err := parseBackup(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Groups) != 1 || cfg.Groups[0].Filter != "DE" {
		t.Fatalf("группа потерялась: %+v", cfg.Groups)
	}
}
