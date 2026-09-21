package media

import (
	"strings"
	"testing"
)

const fsG722PCMA = `INVITE sip:160@84.186.224.78:5060 SIP/2.0
Via: SIP/2.0/UDP 136.243.16.181:5060;branch=z9hG4bKfs
From: <sip:145@botauro.com>;tag=abc
To: <sip:160@botauro.com>
Call-ID: call1
CSeq: 10408102 INVITE
Content-Type: application/sdp
Content-Length: 200

v=0
o=FreeSWITCH 1 1 IN IP4 136.243.16.181
s=FreeSWITCH
c=IN IP4 136.243.16.181
t=0 0
m=audio 19300 RTP/AVP 9 8 101
a=rtpmap:9 G722/8000
a=rtpmap:8 PCMA/8000
a=rtpmap:101 telephone-event/8000
a=fmtp:101 0-16
a=ptime:20
`

func TestPickAnswerCodecG722First(t *testing.T) {
	t.Parallel()
	ans := AnswerFromSIP(fsG722PCMA)
	if ans.PT != 9 || ans.Name != "G722" || ans.Codec != "G722/8000" {
		t.Fatalf("G722 pick: %+v", ans)
	}
	if ans.TelPT != 101 || ans.TelFMTP != "0-16" {
		t.Fatalf("telephone-event: %+v", ans)
	}
	sdp := FormatAnswerSDP(5062, ans)
	if !strings.Contains(sdp, "m=audio 5062 RTP/AVP 9 101") {
		t.Fatalf("m-line: %q", sdp)
	}
	if !strings.Contains(sdp, "a=rtpmap:9 G722/8000") {
		t.Fatalf("rtpmap: %q", sdp)
	}
	if strings.Contains(sdp, "PCMU") || strings.Contains(sdp, "RTP/AVP 0") {
		t.Fatalf("must not answer PCMU: %q", sdp)
	}
}

func TestPickAnswerCodecPCMAOnly(t *testing.T) {
	t.Parallel()
	ans := PickAnswerCodec("m=audio 8000 RTP/AVP 8\na=rtpmap:8 PCMA/8000\n")
	if ans.PT != 8 || ans.Name != "PCMA" {
		t.Fatalf("PCMA: %+v", ans)
	}
}

func TestPickAnswerCodecOpusDynamic(t *testing.T) {
	t.Parallel()
	sdp := "m=audio 9 RTP/AVP 96 101\na=rtpmap:96 opus/48000/2\na=fmtp:96 minptime=10;useinbandfec=1\na=rtpmap:101 telephone-event/8000\n"
	ans := PickAnswerCodec(sdp)
	if ans.PT != 96 || ans.Name != "opus" || ans.Codec != "opus/48000/2" {
		t.Fatalf("opus: %+v", ans)
	}
	got := FormatAnswerSDP(4000, ans)
	if !strings.Contains(got, "a=rtpmap:96 opus/48000/2") || !strings.Contains(got, "a=fmtp:96 minptime=10;useinbandfec=1") {
		t.Fatalf("opus sdp: %q", got)
	}
}

func TestPickAnswerCodecFallbackPCMU(t *testing.T) {
	t.Parallel()
	ans := PickAnswerCodec("m=audio 9 RTP/AVP 18 101\na=rtpmap:18 G729/8000\n")
	if ans.PT != 0 || ans.Name != "PCMU" {
		t.Fatalf("fallback: %+v", ans)
	}
	ans = AnswerFromSIP("INVITE sip:a@b SIP/2.0\r\nContent-Length: 0\r\n\r\n")
	if ans.PT != 0 {
		t.Fatalf("empty body: %+v", ans)
	}
}

func TestPickAnswerCodecStaticPTNoRtpmap(t *testing.T) {
	t.Parallel()
	ans := PickAnswerCodec("m=audio 9 RTP/AVP 8 0\n")
	if ans.PT != 8 || ans.Name != "PCMA" {
		t.Fatalf("static PCMA: %+v", ans)
	}
}
