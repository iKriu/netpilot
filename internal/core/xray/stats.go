package xray

import (
	"context"
	"errors"
	"strings"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protowire"
)

// Статистика Xray — gRPC-сервис StatsService. Сообщения совсем простые,
// поэтому кодируем их вручную, без сгенерированного кода:
//
//	QueryStatsRequest  { string pattern = 1; bool reset = 2; }
//	QueryStatsResponse { repeated Stat stat = 1; }
//	Stat               { string name = 1; int64 value = 2; }
const queryStats = "/xray.app.stats.command.StatsService/QueryStats"

type statsClient struct {
	mu   sync.Mutex
	conn *grpc.ClientConn
}

func (s *statsClient) open(addr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	s.conn = conn
	return nil
}

func (s *statsClient) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
}

// OutboundTraffic возвращает накопленный трафик по выходам: тег → [отдача, загрузка].
func (c *Core) OutboundTraffic(ctx context.Context) (map[string][2]int64, error) {
	c.stats.mu.Lock()
	conn := c.stats.conn
	c.stats.mu.Unlock()
	if conn == nil {
		return nil, errors.New("статистика Xray недоступна")
	}
	req := protowire.AppendTag(nil, 1, protowire.BytesType)
	req = protowire.AppendString(req, "outbound>>>")
	var resp []byte
	if err := conn.Invoke(ctx, queryStats, &req, &resp, grpc.ForceCodec(rawCodec{})); err != nil {
		return nil, err
	}
	stats, err := parseStats(resp)
	if err != nil {
		return nil, err
	}
	out := map[string][2]int64{}
	for name, v := range stats {
		// outbound>>>node-abc>>>traffic>>>uplink
		p := strings.Split(name, ">>>")
		if len(p) != 4 || p[0] != "outbound" {
			continue
		}
		t := out[p[1]]
		if p[3] == "uplink" {
			t[0] = v
		} else {
			t[1] = v
		}
		out[p[1]] = t
	}
	return out, nil
}

func parseStats(b []byte) (map[string]int64, error) {
	out := map[string]int64{}
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		b = b[n:]
		if num != 1 || typ != protowire.BytesType {
			n = protowire.ConsumeFieldValue(num, typ, b)
			if n < 0 {
				return nil, protowire.ParseError(n)
			}
			b = b[n:]
			continue
		}
		msg, n := protowire.ConsumeBytes(b)
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		b = b[n:]
		var name string
		var value int64
		for len(msg) > 0 {
			fnum, ftyp, m := protowire.ConsumeTag(msg)
			if m < 0 {
				return nil, protowire.ParseError(m)
			}
			msg = msg[m:]
			switch {
			case fnum == 1 && ftyp == protowire.BytesType:
				s, k := protowire.ConsumeString(msg)
				if k < 0 {
					return nil, protowire.ParseError(k)
				}
				name, msg = s, msg[k:]
			case fnum == 2 && ftyp == protowire.VarintType:
				v, k := protowire.ConsumeVarint(msg)
				if k < 0 {
					return nil, protowire.ParseError(k)
				}
				value, msg = int64(v), msg[k:]
			default:
				k := protowire.ConsumeFieldValue(fnum, ftyp, msg)
				if k < 0 {
					return nil, protowire.ParseError(k)
				}
				msg = msg[k:]
			}
		}
		out[name] = value
	}
	return out, nil
}

// rawCodec передаёт уже закодированные байты как есть.
type rawCodec struct{}

func (rawCodec) Marshal(v any) ([]byte, error) { return *(v.(*[]byte)), nil }
func (rawCodec) Unmarshal(data []byte, v any) error {
	*(v.(*[]byte)) = append([]byte(nil), data...)
	return nil
}
func (rawCodec) Name() string { return "proto" }
