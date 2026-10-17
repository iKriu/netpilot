#!/bin/sh
# «Провайдер» для стенда: PPPoE-сервер (логин lab-user / lab-pass), DHCP и выход в интернет через NAT.
set -e
apk add --no-cache ppp ppp-pppoe rp-pppoe nftables dnsmasq >/dev/null

ISP_IF=$(ip -o -4 addr show | awk '/10\.99\.0\.2\//{print $2; exit}')

printf '"lab-user" * "lab-pass" *\n' > /etc/ppp/pap-secrets
cp /etc/ppp/pap-secrets /etc/ppp/chap-secrets
cat > /etc/ppp/pppoe-server-options <<EOF
require-pap
lcp-echo-interval 10
lcp-echo-failure 2
ms-dns 1.1.1.1
logfile /tmp/ppp-server.log
EOF

echo 1 > /proc/sys/net/ipv4/ip_forward
nft -f - <<EOF
table ip nat {
	chain post {
		type nat hook postrouting priority srcnat;
		oifname != "$ISP_IF" masquerade
	}
}
EOF

# DHCP для проверки режима «Динамический IP»: шлюз — этот «провайдер».
dnsmasq -k --port=0 --interface="$ISP_IF" --bind-interfaces \
  --dhcp-range=10.99.0.100,10.99.0.150,255.255.255.0,1h \
  --dhcp-option=3,10.99.0.2 --dhcp-option=6,1.1.1.1 &

echo "isp ready on $ISP_IF"
# Режим ядра (-k) с модулем pppoe.so из pppd 2.5: свой rp-pppoe.so пакета
# с этим pppd не совместим, а пользовательскому режиму нужен модуль ppp_async,
# которого нет в ядре Docker Desktop.
PLUGIN=$(ls /usr/lib/pppd/*/pppoe.so | head -1)
syslogd -O /tmp/syslog # журнал pppd — для разбора, если что-то пойдёт не так
exec pppoe-server -F -k -g "$PLUGIN" -q /usr/sbin/pppd -I "$ISP_IF" -L 10.200.0.1 -R 10.200.0.10 -N 20 -O /etc/ppp/pppoe-server-options
