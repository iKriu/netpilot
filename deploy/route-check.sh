#!/bin/sh
# Куда ушли последние соединения: для каждого сайта — выход (прокси или напрямую).
for host in "$@"; do
  line=$(sudo grep -E "outbound/[a-z0-9]+\[[^]]+\]: outbound connection to $host" /var/lib/netpilot/core.log | tail -n 1)
  out=$(echo "$line" | sed -E 's/.*outbound\/([a-z0-9]+)\[([^]]+)\].*/\1 \2/')
  echo "$host -> ${out:-нет соединений}"
done
echo "--- ошибки (последние):"
sudo grep -E "ERROR" /var/lib/netpilot/core.log | tail -n 4 | cut -c1-220
