package core

import (
	"strings"
	"testing"

	"netpilot/internal/model"
)

func TestCheckGroup(t *testing.T) {
	if err := CheckGroup(&model.Group{Name: "Плохая", Filter: "Герм(ания"}); err == nil {
		t.Fatal("ошибка в фильтре пропущена")
	}
	if err := CheckGroup(&model.Group{Name: "  "}); err == nil {
		t.Fatal("пустое название пропущено")
	}
	g := model.Group{Name: " Европа ", Filter: "Германия|Нидерланды", Exclude: "LTE", Type: ""}
	if err := CheckGroup(&g); err != nil || g.Name != "Европа" || g.Type != "auto" {
		t.Fatalf("%v %+v", err, g)
	}
}

func TestBrokenNodeSkipped(t *testing.T) {
	cfg := &model.Config{Nodes: []model.Node{
		{ID: "ok", Name: "Хороший", Protocol: "vless", Server: "1.1.1.1", Port: 443, Security: "reality", PublicKey: "jNXHt1yRo0vDuchQlIP6Z0ZvjT3KtzVI-T4E7RoLJS0"},
		{ID: "bad", Name: "Битый", Protocol: "vless", Server: "1.1.1.1", Port: 443, Security: "reality", PublicKey: "AAAA"},
	}}
	var bad []string
	for _, n := range cfg.Nodes {
		if p := nodeProblem(n); p != "" {
			bad = append(bad, n.ID+": "+p)
		}
	}
	if len(bad) != 1 || !strings.HasPrefix(bad[0], "bad") {
		t.Fatalf("проблемные серверы: %v", bad)
	}
}
