#!/bin/sh
# Обновление NetPilot на сервере: ставит новую программу из /tmp и включает
# режим «шлюз для всей сети» (перехват всех устройств).
set -e
sudo systemctl stop netpilot
sudo install -m 755 /tmp/netpilot-linux-amd64 /usr/local/bin/netpilot

sudo RESET_TARGET="$RESET_TARGET" python3 - <<'EOF'
import json
p = "/var/lib/netpilot/config.json"
c = json.load(open(p))
s = c.setdefault("settings", {})
s["mode"] = "transparent"
s["transparentScope"] = "all"
s["autoStart"] = True
# Для шлюза всей сети: по умолчанию — группа «Авто» (сама выбирает рабочий узел),
# DNS устройств — напрямую, чтобы нерабочий узел не оставил сеть без DNS.
import os
if os.environ.get("RESET_TARGET") == "1":
    s["defaultTarget"] = ""
s.setdefault("dnsViaProxy", False)
json.dump(c, open(p, "w"), indent=2, ensure_ascii=False)
EOF

# Шлюз: пересылка пакетов и без ICMP-редиректов (иначе устройства начнут обходить шлюз).
printf 'net.ipv4.ip_forward=1\nnet.ipv4.conf.all.send_redirects=0\nnet.ipv4.conf.default.send_redirects=0\n' \
  | sudo tee /etc/sysctl.d/99-netpilot.conf >/dev/null
sudo sysctl -p /etc/sysctl.d/99-netpilot.conf >/dev/null

sudo systemctl start netpilot
sleep 6
echo "служба: $(systemctl is-active netpilot)"
sudo journalctl -u netpilot -n 4 --no-pager -o cat
echo "--- правила:"
sudo nft list table ip netpilot | grep -E 'masquerade|@proxy return|tproxy' || true
echo "--- редиректы: $(sysctl -n net.ipv4.conf.all.send_redirects), пересылка: $(sysctl -n net.ipv4.ip_forward)"
