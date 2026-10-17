#!/bin/sh
# Состав автоматических групп: какие серверы попадают в каждую (только чтение).
sudo python3 - <<'EOF'
import json, re
c = json.load(open("/var/lib/netpilot/config.json"))
nodes = c.get("nodes", [])
print("серверов всего:", len(nodes))
for g in c.get("groups", []):
    f = g.get("filter") or ""
    ex = g.get("exclude") or ""
    ids = g.get("nodeIds") or []
    rf = re.compile(f, re.I) if f else None
    rx = re.compile(ex, re.I) if ex else None
    m = [n for n in nodes if ((not ids and not rf) or n["id"] in ids or (rf and rf.search(n["name"])))
         and not (rx and rx.search(n["name"]))]
    print("\n== %s (%s) фильтр=%r исключить=%r: %d" % (g["name"], g.get("type"), f, ex, len(m)))
    for n in m:
        print("  ", n["name"])
EOF
