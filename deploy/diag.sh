#!/bin/sh
# Диагностика NetPilot на сервере: узлы, настройки, последние события ядра.
sudo python3 - <<'EOF'
import json
c = json.load(open("/var/lib/netpilot/config.json"))
for n in c.get("nodes", []):
    print("узел:", n.get("name"), n.get("protocol"), n.get("server"), n.get("port"), n.get("transport"), n.get("security"))
s = c["settings"]
print("остальное:", s.get("defaultAction"), "| DNS прокси:", s.get("dnsRemote"), "| DNS прямой:", s.get("dnsDirect"), "| правил:", len(c.get("rules", [])))
EOF
echo "--- соединения через узлы (последние):"
sudo grep -E "outbound/(vless|vmess|trojan|shadowsocks|hysteria2|tuic)" /var/lib/netpilot/core.log | tail -n 3
echo "--- ошибки:"
sudo grep -iE "error|fail|timeout" /var/lib/netpilot/core.log | tail -n 5
