package backfill

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	mrand "math/rand"
	"time"
)

// sipMsg represents one SIP message in a synthetic call.
type sipMsg struct {
	Offset    time.Duration // offset from call start time
	Direction string        // "send" or "recv"
	Payload   []byte
}

// syntheticCall holds the full set of SIP messages for one backfilled call.
type syntheticCall struct {
	CallID   string
	Start    time.Time
	Duration time.Duration
	Failed   bool
	Status   int
	Messages []sipMsg
}

func generateCallID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		// Fallback to math/rand if crypto/rand fails.
		for i := range b {
			b[i] = byte(mrand.Intn(256))
		}
	}
	return hex.EncodeToString(b) + "@gossipper-backfill"
}

func randomBranch() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "z9hG4bK-bf-" + hex.EncodeToString(b)
}

func randomTag() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func randomDuration(min, max time.Duration) time.Duration {
	if min >= max {
		return min
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(max-min)))
	return min + time.Duration(n.Int64())
}

// synthesizeCall creates a complete SIP dialog with realistic message payloads.
// For failed calls, the dialog terminates at the response stage.
func synthesizeCall(cfg Config, callStart time.Time, callNum int, rng *mrand.Rand) syntheticCall {
	callID := generateCallID()
	fromTag := randomTag()
	toTag := randomTag()
	branch := randomBranch()
	failed := rng.Float64() < cfg.FailRatio

	service := fmt.Sprintf("user%d", rng.Intn(1000))

	var status int
	var callDur time.Duration
	var msgs []sipMsg

	if failed {
		failCodes := []int{486, 503, 408, 480, 487, 404}
		status = failCodes[rng.Intn(len(failCodes))]
		callDur = time.Duration(rng.Int63n(int64(2 * time.Second)))

		msgs = []sipMsg{
			{Offset: 0, Direction: "send", Payload: buildINVITE(cfg, callID, fromTag, branch, service)},
			{Offset: 5 * time.Millisecond, Direction: "recv", Payload: build100Trying(callID, fromTag, branch, cfg)},
			{Offset: time.Duration(50+rng.Intn(200)) * time.Millisecond, Direction: "recv",
				Payload: buildResponse(status, statusReason(status), callID, fromTag, toTag, branch, cfg)},
			{Offset: time.Duration(55+rng.Intn(200)) * time.Millisecond, Direction: "send",
				Payload: buildACK(cfg, callID, fromTag, toTag, branch, service)},
		}
	} else {
		status = 200
		callDur = randomDuration(cfg.CallDurationMin, cfg.CallDurationMax)
		ringDelay := time.Duration(50+rng.Intn(300)) * time.Millisecond
		answerDelay := ringDelay + time.Duration(200+rng.Intn(1000))*time.Millisecond
		ackDelay := answerDelay + 5*time.Millisecond
		byeTime := ackDelay + callDur
		byeResp := byeTime + time.Duration(10+rng.Intn(50))*time.Millisecond

		msgs = []sipMsg{
			{Offset: 0, Direction: "send", Payload: buildINVITE(cfg, callID, fromTag, branch, service)},
			{Offset: 5 * time.Millisecond, Direction: "recv", Payload: build100Trying(callID, fromTag, branch, cfg)},
			{Offset: ringDelay, Direction: "recv", Payload: build180Ringing(callID, fromTag, toTag, branch, cfg)},
			{Offset: answerDelay, Direction: "recv", Payload: build200OK(callID, fromTag, toTag, branch, cfg, "INVITE")},
			{Offset: ackDelay, Direction: "send", Payload: buildACK(cfg, callID, fromTag, toTag, branch, service)},
			{Offset: byeTime, Direction: "send", Payload: buildBYE(cfg, callID, fromTag, toTag, service)},
			{Offset: byeResp, Direction: "recv", Payload: build200OK(callID, fromTag, toTag, randomBranch(), cfg, "BYE")},
		}
	}

	return syntheticCall{
		CallID:   callID,
		Start:    callStart,
		Duration: callDur,
		Failed:   failed,
		Status:   status,
		Messages: msgs,
	}
}

func buildINVITE(cfg Config, callID, fromTag, branch, service string) []byte {
	return []byte(fmt.Sprintf(
		"INVITE sip:%s@%s:%d SIP/2.0\r\n"+
			"Via: SIP/2.0/UDP %s:%d;branch=%s\r\n"+
			"From: <sip:%s@%s:%d>;tag=%s\r\n"+
			"To: <sip:%s@%s:%d>\r\n"+
			"Call-ID: %s\r\n"+
			"CSeq: 1 INVITE\r\n"+
			"Contact: <sip:gossip@%s:%d>\r\n"+
			"Max-Forwards: 70\r\n"+
			"Content-Length: 0\r\n\r\n",
		service, cfg.DstIP, cfg.DstPort,
		cfg.SrcIP, cfg.SrcPort, branch,
		service, cfg.SrcIP, cfg.SrcPort, fromTag,
		service, cfg.DstIP, cfg.DstPort,
		callID,
		cfg.SrcIP, cfg.SrcPort,
	))
}

func build100Trying(callID, fromTag, branch string, cfg Config) []byte {
	return []byte(fmt.Sprintf(
		"SIP/2.0 100 Trying\r\n"+
			"Via: SIP/2.0/UDP %s:%d;branch=%s\r\n"+
			"From: <sip:user@%s:%d>;tag=%s\r\n"+
			"To: <sip:user@%s:%d>\r\n"+
			"Call-ID: %s\r\n"+
			"CSeq: 1 INVITE\r\n"+
			"Content-Length: 0\r\n\r\n",
		cfg.SrcIP, cfg.SrcPort, branch,
		cfg.SrcIP, cfg.SrcPort, fromTag,
		cfg.DstIP, cfg.DstPort,
		callID,
	))
}

func build180Ringing(callID, fromTag, toTag, branch string, cfg Config) []byte {
	return []byte(fmt.Sprintf(
		"SIP/2.0 180 Ringing\r\n"+
			"Via: SIP/2.0/UDP %s:%d;branch=%s\r\n"+
			"From: <sip:user@%s:%d>;tag=%s\r\n"+
			"To: <sip:user@%s:%d>;tag=%s\r\n"+
			"Call-ID: %s\r\n"+
			"CSeq: 1 INVITE\r\n"+
			"Contact: <sip:user@%s:%d>\r\n"+
			"Content-Length: 0\r\n\r\n",
		cfg.SrcIP, cfg.SrcPort, branch,
		cfg.SrcIP, cfg.SrcPort, fromTag,
		cfg.DstIP, cfg.DstPort, toTag,
		callID,
		cfg.DstIP, cfg.DstPort,
	))
}

func build200OK(callID, fromTag, toTag, branch string, cfg Config, method string) []byte {
	return []byte(fmt.Sprintf(
		"SIP/2.0 200 OK\r\n"+
			"Via: SIP/2.0/UDP %s:%d;branch=%s\r\n"+
			"From: <sip:user@%s:%d>;tag=%s\r\n"+
			"To: <sip:user@%s:%d>;tag=%s\r\n"+
			"Call-ID: %s\r\n"+
			"CSeq: 1 %s\r\n"+
			"Contact: <sip:user@%s:%d>\r\n"+
			"Content-Length: 0\r\n\r\n",
		cfg.SrcIP, cfg.SrcPort, branch,
		cfg.SrcIP, cfg.SrcPort, fromTag,
		cfg.DstIP, cfg.DstPort, toTag,
		callID,
		method,
		cfg.DstIP, cfg.DstPort,
	))
}

func buildACK(cfg Config, callID, fromTag, toTag, branch, service string) []byte {
	return []byte(fmt.Sprintf(
		"ACK sip:%s@%s:%d SIP/2.0\r\n"+
			"Via: SIP/2.0/UDP %s:%d;branch=%s\r\n"+
			"From: <sip:%s@%s:%d>;tag=%s\r\n"+
			"To: <sip:%s@%s:%d>;tag=%s\r\n"+
			"Call-ID: %s\r\n"+
			"CSeq: 1 ACK\r\n"+
			"Max-Forwards: 70\r\n"+
			"Content-Length: 0\r\n\r\n",
		service, cfg.DstIP, cfg.DstPort,
		cfg.SrcIP, cfg.SrcPort, branch,
		service, cfg.SrcIP, cfg.SrcPort, fromTag,
		service, cfg.DstIP, cfg.DstPort, toTag,
		callID,
	))
}

func buildBYE(cfg Config, callID, fromTag, toTag, service string) []byte {
	return []byte(fmt.Sprintf(
		"BYE sip:%s@%s:%d SIP/2.0\r\n"+
			"Via: SIP/2.0/UDP %s:%d;branch=%s\r\n"+
			"From: <sip:%s@%s:%d>;tag=%s\r\n"+
			"To: <sip:%s@%s:%d>;tag=%s\r\n"+
			"Call-ID: %s\r\n"+
			"CSeq: 2 BYE\r\n"+
			"Max-Forwards: 70\r\n"+
			"Content-Length: 0\r\n\r\n",
		service, cfg.DstIP, cfg.DstPort,
		cfg.SrcIP, cfg.SrcPort, randomBranch(),
		service, cfg.SrcIP, cfg.SrcPort, fromTag,
		service, cfg.DstIP, cfg.DstPort, toTag,
		callID,
	))
}

func buildResponse(code int, reason, callID, fromTag, toTag, branch string, cfg Config) []byte {
	return []byte(fmt.Sprintf(
		"SIP/2.0 %d %s\r\n"+
			"Via: SIP/2.0/UDP %s:%d;branch=%s\r\n"+
			"From: <sip:user@%s:%d>;tag=%s\r\n"+
			"To: <sip:user@%s:%d>;tag=%s\r\n"+
			"Call-ID: %s\r\n"+
			"CSeq: 1 INVITE\r\n"+
			"Content-Length: 0\r\n\r\n",
		code, reason,
		cfg.SrcIP, cfg.SrcPort, branch,
		cfg.SrcIP, cfg.SrcPort, fromTag,
		cfg.DstIP, cfg.DstPort, toTag,
		callID,
	))
}

func statusReason(code int) string {
	switch code {
	case 100:
		return "Trying"
	case 180:
		return "Ringing"
	case 200:
		return "OK"
	case 404:
		return "Not Found"
	case 408:
		return "Request Timeout"
	case 480:
		return "Temporarily Unavailable"
	case 486:
		return "Busy Here"
	case 487:
		return "Request Terminated"
	case 503:
		return "Service Unavailable"
	default:
		return "Unknown"
	}
}
