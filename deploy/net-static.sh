#!/bin/sh
# Статический адрес шлюза со шлюзом на настоящий роутер. Нужен, когда роутер
# раздаёт этот компьютер как шлюз: иначе по DHCP он получил бы шлюзом сам себя.
# С автоматическим откатом: если через 120 с нет файла /tmp/np-net-ok, всё вернётся на DHCP.
set -e
CON=${CON:-netplan-enp3s0}
ADDR=${ADDR:-192.168.0.77/24}
GW=${GW:-192.168.0.1}
DNS=${DNS:-"192.168.0.1 1.1.1.1"}

rm -f /tmp/np-net-ok
sudo systemd-run --unit=np-net-rollback --on-active=120 /bin/sh -c \
  "test -f /tmp/np-net-ok || { nmcli con mod '$CON' ipv4.method auto ipv4.addresses '' ipv4.gateway '' ipv4.dns '' ipv4.ignore-auto-dns no; nmcli con up '$CON'; }"

sudo nmcli con mod "$CON" ipv4.method manual ipv4.addresses "$ADDR" ipv4.gateway "$GW" \
  ipv4.dns "$DNS" ipv4.ignore-auto-dns yes
# Переподключение отдельно от SSH-сессии: связь может мигнуть.
sudo systemd-run --unit=np-net-apply --collect nmcli con up "$CON"
echo "применено; откат через 120 с, если не подтвердить: touch /tmp/np-net-ok"
