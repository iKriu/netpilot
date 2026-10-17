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
ENTRYPOINT ["netpilot"]
