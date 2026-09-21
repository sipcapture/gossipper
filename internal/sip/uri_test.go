package sip

import "testing"

func TestRequestURIUser(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"sip:1001@192.168.1.20:5060", "1001"},
		{"<sip:1002@pbx.local>", "1002"},
		{"sips:1003@host", "1003"},
		{"sip:192.168.1.20:5060", ""},
		{"tel:+1555", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := RequestURIUser(tc.in); got != tc.want {
			t.Errorf("RequestURIUser(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}
