package media

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/adubovikov/gossipper/internal/sip"
)

func ParseAudioEndpoint(msg sip.Message, fallbackIP string) (Endpoint, error) {
	body := strings.ReplaceAll(msg.Body, "\r\n", "\n")
	lines := strings.Split(body, "\n")
	ip := fallbackIP
	port := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "c="):
			fields := strings.Fields(strings.TrimPrefix(line, "c="))
			if len(fields) >= 3 {
				ip = fields[2]
			}
		case strings.HasPrefix(line, "m=audio "):
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				value, err := strconv.Atoi(fields[1])
				if err == nil {
					port = value
				}
			}
		}
	}
	if ip == "" || port <= 0 {
		return Endpoint{}, fmt.Errorf("audio SDP endpoint not found")
	}
	return Endpoint{IP: ip, Port: port}, nil
}
