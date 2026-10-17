#!/bin/sh
# Правила «по спискам»: звонки и Re:filter через прокси, остальное напрямую.
# Существующие правила сохраняются (добавленные ранее с теми же именами — заменяются).
set -e
sudo systemctl stop netpilot
sudo install -m 755 /tmp/netpilot-linux-amd64 /usr/local/bin/netpilot
sudo python3 - <<'EOF'
import json, secrets
p = "/var/lib/netpilot/config.json"
c = json.load(open(p))

def rule(name, what, action="proxy"):
    w = {"apps": [], "lists": [], "domains": [], "ips": [], "geosite": [], "geoip": [], "ports": [], "network": ""}
    w.update(what)
    return {"id": secrets.token_hex(5), "name": name, "enabled": True,
            "who": {"deviceIds": [], "tags": [], "cidrs": []}, "what": w,
            "where": {"action": action, "target": ""}}

ours = [
    rule("Telegram, Discord, FaceTime", {"apps": ["telegram", "discord", "facetime"]}),
    rule("FaceTime — голос и видео", {"ips": ["17.0.0.0/8"], "network": "udp"}),
    rule("Заблокированное в РФ (Re:filter)", {"lists": ["refilter-domains", "refilter-ips"]}),
]
names = {r["name"] for r in ours}
kept = [r for r in c.get("rules", []) if r.get("name") not in names]
for r in kept:
    print("сохранено правило:", r.get("name"), "->", r.get("where", {}).get("action"))
c["rules"] = ours + kept
c["settings"]["defaultAction"] = "direct"
json.dump(c, open(p, "w"), indent=2, ensure_ascii=False)
print("правил:", len(c["rules"]), "| остальной трафик: напрямую")
EOF
sudo systemctl start netpilot
sleep 25
echo "служба: $(systemctl is-active netpilot)"
sudo journalctl -u netpilot -n 6 --no-pager -o cat
ls -la /var/lib/netpilot/lists/ 2>/dev/null
