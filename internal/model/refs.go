package model

import "slices"

// References перечисляет места, которые ссылаются на узел или группу id:
// правила, маршруты устройств, сервер по умолчанию и составы групп.
// API показывает этот список перед удалением: после удаления такие ссылки
// молча переключатся на «по умолчанию», и человек должен об этом знать.
func References(cfg *Config, id string) []string {
	if cfg == nil || id == "" {
		return nil
	}
	var out []string
	if cfg.Settings.DefaultTarget == id {
		out = append(out, "сервер по умолчанию в настройках")
	}
	for i := range cfg.Rules {
		if r := &cfg.Rules[i]; r.Where.Target == id {
			out = append(out, ruleLabel(r))
		}
	}
	for i := range cfg.Devices {
		if d := &cfg.Devices[i]; d.RouteTarget == id {
			out = append(out, deviceLabel(d))
		}
	}
	for i := range cfg.Groups {
		if g := &cfg.Groups[i]; g.ID != id && slices.Contains(g.NodeIDs, id) {
			out = append(out, groupLabel(g))
		}
	}
	return out
}
