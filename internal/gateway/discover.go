package gateway

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// LocalAddr is a host IPv4 on a named interface (Contact candidate).
type LocalAddr struct {
	IP    string `json:"ip"`
	Iface string `json:"iface,omitempty"`
}

// IPHints is Autodiscover payload for advertised_ip.
type IPHints struct {
	Local         []LocalAddr `json:"local"`
	External      string      `json:"external,omitempty"`
	ExternalError string      `json:"external_error,omitempty"`
}

var (
	defaultExternalClient = &http.Client{Timeout: 3 * time.Second}
	defaultExternalURLs   = []string{
		"https://api.ipify.org",
		"https://icanhazip.com",
	}
)

func usableIPv4(ip net.IP) bool {
	if ip == nil {
		return false
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	return !ip4.IsLoopback() && !ip4.IsLinkLocalUnicast() && !ip4.IsUnspecified() && !ip4.IsMulticast()
}

// ListLocalIPv4 returns non-loopback IPv4 addresses on up interfaces.
func ListLocalIPv4() ([]LocalAddr, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]LocalAddr, 0)
	seen := map[string]struct{}{}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if !usableIPv4(ip) {
				continue
			}
			s := ip.To4().String()
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			out = append(out, LocalAddr{IP: s, Iface: iface.Name})
		}
	}
	return out, nil
}

// ParsePublicIP extracts a unicast public IPv4 from a STUN/HTTP echo body.
func ParsePublicIP(body string) (string, error) {
	raw := strings.TrimSpace(body)
	if i := strings.IndexAny(raw, " \t\r\n,"); i >= 0 {
		raw = raw[:i]
	}
	ip := net.ParseIP(raw)
	if ip == nil {
		return "", fmt.Errorf("not an IP address")
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return "", fmt.Errorf("not an IPv4 address")
	}
	if ip4.IsLoopback() || ip4.IsPrivate() || ip4.IsUnspecified() || ip4.IsLinkLocalUnicast() {
		return "", fmt.Errorf("not a public IPv4 address")
	}
	return ip4.String(), nil
}

func fetchPublicIP(ctx context.Context, client *http.Client, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "gossipper")
	req.Header.Set("Accept", "text/plain")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return "", err
	}
	return ParsePublicIP(string(b))
}

// LookupExternalIP queries echo endpoints until one returns a public IPv4.
func LookupExternalIP(ctx context.Context, client *http.Client, urls []string) (string, error) {
	if client == nil {
		client = defaultExternalClient
	}
	if len(urls) == 0 {
		urls = defaultExternalURLs
	}
	var last error
	for _, u := range urls {
		ip, err := fetchPublicIP(ctx, client, u)
		if err == nil {
			return ip, nil
		}
		last = err
	}
	if last == nil {
		last = fmt.Errorf("no external IP endpoints")
	}
	return "", last
}

// DiscoverIPs lists local IPv4s and best-effort public IPv4 for Contact.
func DiscoverIPs(ctx context.Context) IPHints {
	if ctx == nil {
		ctx = context.Background()
	}
	hints := IPHints{Local: []LocalAddr{}}
	if local, err := ListLocalIPv4(); err == nil {
		hints.Local = local
	}
	extCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	ext, err := LookupExternalIP(extCtx, nil, nil)
	if err != nil {
		hints.ExternalError = err.Error()
		return hints
	}
	hints.External = ext
	return hints
}
