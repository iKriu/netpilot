#!/bin/sh
# Группа «Европа (авто)» — быстрейший из европейских узлов — и выбор её по умолчанию.
# Российские узлы для обхода блокировок не подходят, а «Авто» по всем узлам
# выбирает ближайший, то есть московский.
set -e
sudo systemctl stop netpilot
sudo python3 - <<'EOF'
import json, secrets
p = "/var/lib/netpilot/config.json"
c = json.load(open(p))
groups = c.setdefault("groups", [])
g = next((g for g in groups if g.get("name") == "Европа (авто)"), None)
if g is None:
    g = {"id": secrets.token_hex(5), "name": "Европа (авто)", "type": "auto", "nodeIds": []}
    groups.append(g)
g["filter"] = "Германия|Нидерланды|Финляндия|Швеция|Эстония|Латвия|Польша"
c["settings"]["defaultTarget"] = g["id"]
json.dump(c, open(p, "w"), indent=2, ensure_ascii=False)
print("группа:", g["id"])
EOF
sudo systemctl start netpilot
