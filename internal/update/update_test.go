package update

import "testing"

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.1.1", "0.1.0", true}, {"v0.2.0", "0.1.9", true}, {"0.1.0", "0.1.0", false},
		{"0.1.0", "0.1.1", false}, {"0.10.0", "0.9.9", true}, {"1.0", "0.9.9", true},
		{"0.1.0", "dev", true}, {"latest", "0.1.0", false}, {"0.2.0-beta", "0.1.0", true},
	} {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("newer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestFindSum(t *testing.T) {
	sums := []byte("aaa  netpilot-linux-amd64\nbbb *netpilot-windows-amd64.exe\n")
	if findSum(sums, "netpilot-linux-amd64") != "aaa" || findSum(sums, "netpilot-windows-amd64.exe") != "bbb" || findSum(sums, "x") != "" {
		t.Fatal("findSum")
	}
}
