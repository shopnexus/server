package telemetry

import "testing"

func TestParseCgroupBytes(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"536870912\n", 536870912, true},
		{"max\n", 0, false},
		{"", 0, false},
		{"0\n", 0, false},
	}
	for _, c := range cases {
		got, ok := parseCgroupBytes(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("parseCgroupBytes(%q) = %d, %v; want %d, %v", c.in, got, ok, c.want, c.ok)
		}
	}
}
