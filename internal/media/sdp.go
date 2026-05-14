package media

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sipcapture/gossipper/internal/sip"
)

func ParseAudioEndpoint(msg sip.Message, fallbackIP string) (Endpoint, error) {
	return ParseMediaEndpoint(msg, fallbackIP, "audio")
}

func ParseMediaEndpoint(msg sip.Message, fallbackIP string, mediaType string) (Endpoint, error) {
	raw := EffectiveMediaSDPBody(msg)
	body := strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(body, "\n")
	ip := fallbackIP
	port := 0
	mediaPrefix := "m=" + strings.ToLower(strings.TrimSpace(mediaType)) + " "
	if mediaPrefix == "m= " {
		return Endpoint{}, fmt.Errorf("media type is required")
	}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "c="):
			fields := strings.Fields(strings.TrimPrefix(line, "c="))
			if len(fields) >= 3 {
				ip = fields[2]
			}
		case strings.HasPrefix(strings.ToLower(line), mediaPrefix):
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				value, err := strconv.Atoi(fields[1])
				if err == nil {
					port = value
				}
			}
		}
	}
	var iceTyp string
	mt := strings.ToLower(strings.TrimSpace(mediaType))
	if mt == "audio" || mt == "video" || mt == "image" {
		ApplyBundleMediaEndpointIfNeeded(body, mt, &ip, &port)
	}
	if mt == "audio" || mt == "video" || mt == "image" {
		if iceIP, icePort, typ, ok := PickMediaICERTPEndpoint(body, mt, ip, port); ok {
			ip, port = iceIP, icePort
			iceTyp = typ
		}
	}
	if ip == "" || port <= 0 {
		return Endpoint{}, fmt.Errorf("%s SDP endpoint not found", strings.ToLower(strings.TrimSpace(mediaType)))
	}
	return Endpoint{IP: ip, Port: port, ICECandidateTyp: iceTyp}, nil
}
