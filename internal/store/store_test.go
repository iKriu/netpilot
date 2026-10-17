package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"netpilot/internal/model"
)

func readSchema(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var head struct {
		Schema   int            `json:"schema"`
		Settings map[string]any `json:"settings"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		t.Fatal(err)
	}
	if head.Settings == nil {
		t.Fatal("в файле нет полей конфигурации на верхнем уровне")
	}
	return head.Schema
}

func TestOpenNewWritesSchema(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, func(c *model.Config) { c.Settings.ListenAddr = "0.0.0.0" })
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get().Settings.ListenAddr; got != "0.0.0.0" {
		t.Fatalf("firstRun не применён: %q", got)
	}
	if v := readSchema(t, filepath.Join(dir, "config.json")); v != model.SchemaVersion {
		t.Fatalf("schema %d, ожидалось %d", v, model.SchemaVersion)
	}
	// Повторное открытие читает то же самое и не вызывает firstRun.
	s2, err := Open(dir, func(*model.Config) { t.Fatal("firstRun при существующем файле") })
	if err != nil {
		t.Fatal(err)
	}
	if s2.Get().Settings.ListenAddr != "0.0.0.0" {
		t.Fatal("настройки не сохранились")
	}
}

func TestOpenMigratesLegacyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	// Файл старой версии: без schema и без части полей.
	legacy := `{"settings":{"mixedPort":8080},"nodes":[{"id":"n1","name":"A","protocol":"vless","server":"a.example","port":443,"subscriptionId":"s"}],
		"devices":[{"id":"d1","name":"ТВ","firstSeen":1700000000123}]}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := s.Get()
	if c.Settings.MixedPort != 8080 || c.Settings.TProxyPort != 7893 || c.Settings.Mode != "proxy" {
		t.Fatalf("поверх значений по умолчанию не наложилось: %+v", c.Settings)
	}
	if len(c.Nodes) != 1 || c.Nodes[0].Server != "a.example" || c.Devices[0].FirstSeen != 1700000000123 {
		t.Fatalf("данные потерялись при миграции: %+v %+v", c.Nodes, c.Devices)
	}
	if v := readSchema(t, path); v != model.SchemaVersion {
		t.Fatalf("после миграции schema %d", v)
	}
	if bak, err := os.ReadFile(path + ".bak"); err != nil || string(bak) != legacy {
		t.Fatalf("копия прежнего файла: %v", err)
	}
}

func TestOpenRefusesNewerSchema(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{"schema":` + strconv.Itoa(model.SchemaVersion+1) + `,"settings":{"mixedPort":7890},"futureField":[1,2]}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(dir, nil)
	if err == nil || !strings.Contains(err.Error(), "более новой версией") {
		t.Fatalf("ожидался отказ запуска, получено %v", err)
	}
	// Файл не тронут: новая версия программы прочитает его целиком.
	if got, _ := os.ReadFile(path); string(got) != data {
		t.Fatal("файл новой версии перезаписан")
	}
}

func TestOpenCorruptFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir, nil); err == nil {
		t.Fatal("повреждённый файл принят")
	}
}

func TestSnapshotsAreImmutable(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(c *model.Config) error {
		c.Groups = append(c.Groups, model.Group{ID: "g", Name: "G", NodeIDs: []string{"a", "b"}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before := s.Get()
	if s.Get() != before {
		t.Fatal("Get без изменений должен возвращать тот же снимок")
	}
	if err := s.Update(func(c *model.Config) error {
		c.Groups[0].NodeIDs[0] = "changed" // срез внутри структуры — копия обязана быть глубокой
		c.Settings.MixedPort = 1234
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if before.Groups[0].NodeIDs[0] != "a" || before.Settings.MixedPort == 1234 {
		t.Fatal("Update изменил ранее выданный снимок")
	}
	if after := s.Get(); after.Groups[0].NodeIDs[0] != "changed" || after.Settings.MixedPort != 1234 {
		t.Fatal("новый снимок не опубликован")
	}

	// Ошибка в fn — снимок прежний.
	cur := s.Get()
	boom := errors.New("boom")
	if err := s.Update(func(c *model.Config) error { c.Settings.MixedPort = 1; return boom }); err != boom {
		t.Fatalf("ошибка fn потерялась: %v", err)
	}
	if s.Get() != cur {
		t.Fatal("неудачный Update заменил снимок")
	}

	// Replace копирует аргумент.
	in := model.Defaults()
	in.Rules = []model.Rule{{ID: "r", Who: model.Who{Tags: []string{"x"}}}}
	if err := s.Replace(in); err != nil {
		t.Fatal(err)
	}
	in.Rules[0].Who.Tags[0] = "y"
	if s.Get().Rules[0].Who.Tags[0] != "x" {
		t.Fatal("Replace делит память с аргументом")
	}
}

// Одновременные чтения и записи: каждый снимок согласован (поля, изменённые
// одним Update, видны вместе) и не меняется, пока его читают.
// Полезнее всего под go test -race.
func TestConcurrentGetUpdate(t *testing.T) {
	s, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	const writers, perWriter, readers = 4, 25, 8
	stop := make(chan struct{})
	var rwg, wwg sync.WaitGroup
	errc := make(chan string, readers)
	for range readers {
		rwg.Add(1)
		go func() {
			defer rwg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				c := s.Get()
				n := len(c.Nodes)
				// Инвариант всех Update: число узлов = MixedPort-1000, у каждого узла
				// порт = его номер.
				if c.Settings.MixedPort != 7890 && c.Settings.MixedPort-1000 != n {
					errc <- "несогласованный снимок"
					return
				}
				for i, nd := range c.Nodes {
					if nd.Port != i+1 {
						errc <- "узлы снимка изменились при чтении"
						return
					}
				}
				if len(c.Nodes) != n {
					errc <- "снимок изменился при чтении"
					return
				}
			}
		}()
	}
	for range writers {
		wwg.Add(1)
		go func() {
			defer wwg.Done()
			for range perWriter {
				_ = s.Update(func(c *model.Config) error {
					c.Nodes = append(c.Nodes, model.Node{ID: NewID(), Port: len(c.Nodes) + 1})
					c.Settings.MixedPort = 1000 + len(c.Nodes)
					return nil
				})
			}
		}()
	}
	wwg.Wait()
	close(stop)
	rwg.Wait()
	select {
	case msg := <-errc:
		t.Fatal(msg)
	default:
	}
	if got := len(s.Get().Nodes); got != writers*perWriter {
		t.Fatalf("потеряны обновления: %d из %d", got, writers*perWriter)
	}
}
