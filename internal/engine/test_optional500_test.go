//go:build ignore

package main

import (
"context"
"fmt"
"net"
"strings"
"time"

"github.com/sipcapture/gossipper/internal/engine"
"github.com/sipcapture/gossipper/internal/scenario"
"github.com/sipcapture/gossipper/internal/sip"
)

func main() {
serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
if err != nil {
panic(err)
}
defer serverConn.Close()

serverErr := make(chan error, 1)
go func() {
buffer := make([]byte, 8192)
_ = serverConn.SetReadDeadline(time.Now().Add(5 * time.Second))
n, addr, err := serverConn.ReadFromUDP(buffer)
if err != nil {
serverErr <- err
return
}
invite := sip.GetMessage()
defer sip.PutMessage(invite)
if err := sip.ParseInto(invite, buffer[:n]); err != nil {
serverErr <- err
return
}
via, _ := sip.Header(invite.Headers, "Via")
from, _ := sip.Header(invite.Headers, "From")
to, _ := sip.Header(invite.Headers, "To")
callID, _ := sip.Header(invite.Headers, "Call-ID")
cseq, _ := sip.Header(invite.Headers, "CSeq")

// 100 Trying
r100 := fmt.Sprintf(
"SIP/2.0 100 Trying\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
via, from, to, callID, cseq,
)
if _, err := serverConn.WriteToUDP([]byte(r100), addr); err != nil {
serverErr <- err
return
}
time.Sleep(50 * time.Millisecond)

// 500 Server Error
r500 := fmt.Sprintf(
"SIP/2.0 500 Server Error\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=err\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
via, from, to, callID, cseq,
)
if _, err := serverConn.WriteToUDP([]byte(r500), addr); err != nil {
serverErr <- err
return
}

// Drain any retransmissions
for i := 0; i < 10; i++ {
_ = serverConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
n2, addr2, err2 := serverConn.ReadFromUDP(buffer)
if err2 != nil {
break
}
msg := sip.GetMessage()
_ = sip.ParseInto(msg, buffer[:n2])
method := strings.ToUpper(msg.Method)
fmt.Printf("SERVER: received retransmission: %s\n", method)
if method == "ACK" {
fmt.Println("SERVER: got ACK for error response (good)")
}
sip.PutMessage(msg)
_ = addr2
}
serverErr <- nil
}()

sc, err := scenario.ParseString(`<?xml version="1.0" encoding="UTF-8"?>
<scenario name="optional-500-test">
  <send retrans="500"><![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: client <sip:client@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
Content-Length: 0

]]></send>
  <recv response="100" optional="true"/>
  <recv response="180" optional="true"/>
  <recv response="183" optional="true"/>
  <recv response="500" optional="true" next="call_failed"/>
  <recv response="200"/>

  <label id="call_failed"/>
</scenario>`)
if err != nil {
panic(fmt.Sprintf("ParseString: %v", err))
}

app := engine.New(engine.Config{
Scenario:      sc,
Transport:     "u1",
LocalIP:       "127.0.0.1",
RemoteHost:    "127.0.0.1",
RemotePort:    serverConn.LocalAddr().(*net.UDPAddr).Port,
Service:       "echo",
Rate:          100,
TotalCalls:    1,
MaxConcurrent: 1,
DefaultRecvTO: 2 * time.Second,
})

started := time.Now()
ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
defer cancel()
err = app.Run(ctx)
elapsed := time.Since(started)
fmt.Printf("Run completed in %v, err=%v\n", elapsed, err)
if sErr := <-serverErr; sErr != nil {
fmt.Printf("Server error: %v\n", sErr)
}
}
