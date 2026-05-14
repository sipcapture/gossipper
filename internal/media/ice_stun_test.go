package media

import (
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/pion/stun/v3"
)

func TestTryICEStunBindingResponse(t *testing.T) {
	serverConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer serverConn.Close()

	clientConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()

	from := clientConn.LocalAddr().(*net.UDPAddr)

	s := NewSession()
	s.iceLocalUfrag = "locfrag"
	s.iceLocalPwd = "locpwd123456789012345678"
	s.iceRemoteUfrag = "remfrag"
	s.iceRemotePwd = "rempwd123456789012345678"

	req, err := stun.Build(stun.BindingRequest, stun.TransactionID,
		stun.NewUsername("locfrag:remfrag"),
		stun.NewShortTermIntegrity("locpwd123456789012345678"),
		stun.Fingerprint,
	)
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 1)
	go func() {
		buf := make([]byte, 2048)
		_ = clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, addr, rerr := clientConn.ReadFromUDP(buf)
		if rerr != nil {
			errCh <- rerr
			return
		}
		if addr.String() != serverConn.LocalAddr().String() {
			errCh <- fmt.Errorf("wrong source %s", addr)
			return
		}
		var res stun.Message
		if rerr := stun.Decode(buf[:n], &res); rerr != nil {
			errCh <- rerr
			return
		}
		if res.Type != stun.BindingSuccess {
			errCh <- errors.New("not binding success")
			return
		}
		errCh <- nil
	}()

	if !s.tryICEStunBindingResponse(serverConn, req.Raw, from) {
		t.Fatal("expected STUN handled")
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}

func TestSetRemoteIcePreservesOnEmptyFragment(t *testing.T) {
	t.Parallel()
	s := NewSession()
	s.SetRemoteIceFromSDP("m=audio 9 UDP/TLS/RTP/SAVPF 0\r\na=ice-ufrag:aa\r\na=ice-pwd:bbbbbbbbbbbbbbbbbbbb\r\n")
	if !s.hasRemoteICE() {
		t.Fatal("expected remote ICE set")
	}
	s.SetRemoteIceFromSDP("a=candidate:1 1 udp 1 1.1.1.1 1 typ host\r\n")
	if !s.hasRemoteICE() {
		t.Fatal("expected remote ICE preserved after candidate-only body")
	}
}
