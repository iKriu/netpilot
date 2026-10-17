#!/bin/sh
# Строки журнала ядра по шаблону: sh grep-log.sh 'rutracker|instagram' [сколько]
sudo grep -E "$1" /var/lib/netpilot/core.log | tail -n "${2:-8}" | cut -c1-200
