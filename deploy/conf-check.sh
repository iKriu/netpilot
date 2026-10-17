#!/bin/sh
# Правила маршрутизации и выбранный узел в итоговом конфиге sing-box на сервере.
sudo python3 - <<'EOF'
import json
c = json.load(open("/var/lib/netpilot/sing-box.json"))
print("правила:")
for r in c["route"]["rules"]:
    print("  ", json.dumps(r, ensure_ascii=False)[:160])
print("final:", c["route"]["final"])
for o in c["outbounds"]:
    if o["tag"] == "node-4f08417242":
        o = dict(o); o.pop("uuid", None)
        print("узел:", json.dumps(o, ensure_ascii=False))
    if o["tag"] == "proxy":
        print("proxy default:", o.get("default"), "вариантов:", len(o["outbounds"]))
EOF
