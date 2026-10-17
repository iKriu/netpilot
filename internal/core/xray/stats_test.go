package xray

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func TestParseStats(t *testing.T) {
	stat := func(name string, v uint64) []byte {
		var m []byte
		m = protowire.AppendTag(m, 1, protowire.BytesType)
		m = protowire.AppendString(m, name)
		m = protowire.AppendTag(m, 2, protowire.VarintType)
		m = protowire.AppendVarint(m, v)
		out := protowire.AppendTag(nil, 1, protowire.BytesType)
		return protowire.AppendBytes(out, m)
	}
	resp := append(stat("outbound>>>node-a>>>traffic>>>uplink", 100), stat("outbound>>>node-a>>>traffic>>>downlink", 5000)...)
	got, err := parseStats(resp)
	if err != nil || got["outbound>>>node-a>>>traffic>>>downlink"] != 5000 || len(got) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
}
