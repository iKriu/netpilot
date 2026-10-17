package core

import (
	"slices"
	"testing"

	"netpilot/internal/model"
)

func TestGroupNodesExclude(t *testing.T) {
	cfg := &model.Config{Nodes: []model.Node{
		{ID: "a", Name: "🇩🇪 Германия"},
		{ID: "b", Name: "🇩🇪 0.1X - LTE №99 - Германия"},
		{ID: "c", Name: "🇳🇱 LTE Авто - Нидерланды"},
		{ID: "d", Name: "🇳🇱 Нидерланды #2"},
		{ID: "e", Name: "🇷🇺 Москва"},
	}}
	g := model.Group{Name: "Европа (авто)", Filter: "Германия|Нидерланды", Exclude: "lte"}
	got, err := GroupNodes(cfg, g)
	if err != nil || !slices.Equal(got, []string{"a", "d"}) {
		t.Fatalf("GroupNodes = %v, %v", got, err)
	}
	// Без фильтра — все серверы, кроме исключённых.
	got, _ = GroupNodes(cfg, model.Group{Exclude: "LTE"})
	if !slices.Equal(got, []string{"a", "d", "e"}) {
		t.Fatalf("без фильтра: %v", got)
	}
	if _, err := GroupNodes(cfg, model.Group{Name: "x", Exclude: "("}); err == nil {
		t.Fatal("ошибка в исключении не замечена")
	}
}
