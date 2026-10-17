#!/bin/sh
# Замена программы без изменения настроек: sh install-binary.sh
# Новая сборка — /tmp/netpilot-linux-amd64, старая остаётся в /usr/local/bin/netpilot.bak.
set -e
new=/tmp/netpilot-linux-amd64
chmod 755 "$new"
ver=$("$new" -version)
echo "новая версия: $ver"
sudo cp /usr/local/bin/netpilot /usr/local/bin/netpilot.bak
sudo install -m 755 "$new" /usr/local/bin/.netpilot-new
sudo mv /usr/local/bin/.netpilot-new /usr/local/bin/netpilot
sudo systemctl restart netpilot
sleep 8
echo "служба: $(systemctl is-active netpilot)"
sudo journalctl -u netpilot -n 5 --no-pager -o cat
curl -s -o /dev/null -w "интерфейс: HTTP %{http_code}\n" http://127.0.0.1:2017/api/auth/state
