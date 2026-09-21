package gateway

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUsableIPv4(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", false},
		{"::1", false},
		{"169.254.1.1", false},
		{"0.0.0.0", false},
		{"192.168.1.20", true},
		{"10.0.0.5", true},
		{"8.8.8.8", true},
		{"2001:db8::1", false},
	}
	for _, tc := range cases {
		if got := usableIPv4(net.ParseIP(tc.ip)); got != tc.want {
			t.Errorf("usableIPv4(%s)=%v want %v", tc.ip, got, tc.want)
		}
	}
}

func TestParsePublicIP(t *testing.T) {
	t.Parallel()
	ip, err := ParsePublicIP("  203.0.113.10 \n")
	if err != nil || ip != "203.0.113.10" {
		t.Fatalf("got %q err=%v", ip, err)
	}
	if _, err := ParsePublicIP("not-an-ip"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := ParsePublicIP("127.0.0.1"); err == nil {
		t.Fatal("loopback must be rejected")
	}
	if _, err := ParsePublicIP("10.0.0.1"); err == nil {
		t.Fatal("private must be rejected")
	}
}

func TestLookupExternalIP(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("203.0.113.44\n"))
	}))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ip, err := LookupExternalIP(ctx, srv.Client(), []string{srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if ip != "203.0.113.44" {
		t.Fatalf("ip=%q", ip)
	}
}

func TestLookupExternalIPFallsBack(t *testing.T) {
	t.Parallel()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	t.Cleanup(bad.Close)
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("198.51.100.9"))
	}))
	t.Cleanup(good.Close)
	ip, err := LookupExternalIP(context.Background(), good.Client(), []string{bad.URL, good.URL})
	if err != nil || ip != "198.51.100.9" {
		t.Fatalf("ip=%q err=%v", ip, err)
	}
}

func TestListLocalIPv4SkipsLoopback(t *testing.T) {
	t.Parallel()
	addrs, err := ListLocalIPv4()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range addrs {
		ip := net.ParseIP(a.IP)
		if ip == nil || !usableIPv4(ip) {
			t.Fatalf("unexpected addr %+v", a)
		}
	}
}
