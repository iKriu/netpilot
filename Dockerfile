FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath \
      -tags with_quic,with_utls,with_clash_api \
      -ldflags "-s -w -X netpilot/internal/app.Version=$(cat VERSION)" \
      -o /out/netpilot ./cmd/netpilot

FROM alpine:3.22
# nftables и iproute2 — прозрачный режим; ppp, ppp-pppoe, dnsmasq — режим роутера
# (udhcpc для подключения по DHCP уже есть в busybox)
RUN apk add --no-cache ca-certificates tzdata nftables iproute2 ppp ppp-pppoe dnsmasq
COPY --from=build /out/netpilot /usr/local/bin/netpilot
ENV NETPILOT_DATA=/data \
    NETPILOT_LISTEN=0.0.0.0:2017 \
    NETPILOT_PROXY_LISTEN=0.0.0.0
VOLUME /data
# 2017 — веб-интерфейс, 7890 — HTTP/SOCKS5 прокси
EXPOSE 2017 7890
# Живость — по открытому без входа /api/auth/state (wget есть в busybox alpine).
# Если NETPILOT_LISTEN сменён на другой порт, поправьте адрес и здесь.
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD wget -q -T 4 -O /dev/null http://127.0.0.1:2017/api/auth/state || exit 1
# USER не задаём сознательно: nftables/TPROXY, pppd и dnsmasq требуют root
# (вместе с NET_ADMIN в docker-compose.gateway.yml). В обычном режиме прокси
# контейнер без лишних capabilities, так что root внутри ограничен Docker'ом.
ENTRYPOINT ["netpilot"]
