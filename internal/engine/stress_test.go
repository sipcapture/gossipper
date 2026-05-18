package engine

import (
"context"
"fmt"
"net"
"os"
"strings"
"sync/atomic"
"testing"
"time"

"github.com/sipcapture/gossipper/internal/scenario"
"github.com/sipcapture/gossipper/internal/sip"
)

// TestStressHighCPSCallsComplete verifies that under high CPS with many
// concurrent calls, INVITEs are actually sent and calls complete successfully.
func TestStressHighCPSCallsComplete(t *testing.T) {
const (
totalCalls    = 50
maxConcurrent = 20
rate          = 500.0 // high rate to make test fast
)

serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
if err != nil {
t.Fatalf("ListenUDP() error = %v", err)
}
defer serverConn.Close()

var inviteCount atomic.Int64
done := make(chan struct{})
go func() {
defer close(done)
buffer := make([]byte, 65535)
for {
_ = serverConn.SetReadDeadline(time.Now().Add(5 * time.Second))
n, addr, err := serverConn.ReadFromUDP(buffer)
if err != nil {
return
}
msg := sip.GetMessage()
if err := sip.ParseInto(msg, buffer[:n]); err != nil {
sip.PutMessage(msg)
continue
}
callID, _ := sip.Header(msg.Headers, "Call-ID")
from, _ := sip.Header(msg.Headers, "From")
to, _ := sip.Header(msg.Headers, "To")
via, _ := sip.Header(msg.Headers, "Via")
cseq, _ := sip.Header(msg.Headers, "CSeq")

switch strings.ToUpper(msg.Method) {
case "INVITE":
inviteCount.Add(1)
response := fmt.Sprintf(
"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContact: <sip:127.0.0.1:%d>\r\nContent-Length: 0\r\n\r\n",
via, from, to, callID, cseq, serverConn.LocalAddr().(*net.UDPAddr).Port,
)
_, _ = serverConn.WriteToUDP([]byte(response), addr)
case "ACK":
// ignore
case "BYE":
response := fmt.Sprintf(
"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
via, from, to, callID, cseq,
)
_, _ = serverConn.WriteToUDP([]byte(response), addr)
}
sip.PutMessage(msg)
}
}()

sc, err := scenario.ParseFile("../../testdata/scenarios/basic_uac.xml")
if err != nil {
t.Fatalf("ParseFile() error = %v", err)
}

app := New(Config{
Scenario:      sc,
Transport:     "u1",
LocalIP:       "127.0.0.1",
RemoteHost:    "127.0.0.1",
RemotePort:    serverConn.LocalAddr().(*net.UDPAddr).Port,
Service:       "echo",
Rate:          rate,
TotalCalls:    totalCalls,
MaxConcurrent: maxConcurrent,
DefaultPause:  10 * time.Millisecond,
DefaultRecvTO: 2 * time.Second,
})

ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

if err := app.Run(ctx); err != nil {
t.Fatalf("Run() error = %v", err)
}

summary := app.Stats().Snapshot()
t.Logf("Results: success=%d failed=%d invites_received=%d",
summary.SuccessCalls, summary.FailedCalls, inviteCount.Load())

if inviteCount.Load() == 0 {
t.Fatal("BUG: not a single INVITE was sent!")
}
if summary.SuccessCalls != totalCalls {
t.Fatalf("expected %d successful calls, got %d (failed=%d)",
totalCalls, summary.SuccessCalls, summary.FailedCalls)
}
}

// TestStressHighCPSHighConcurrency reproduces the user scenario: high CPS + trace_msg enabled.
func TestStressHighCPSHighConcurrency(t *testing.T) {
const (
totalCalls    = 200
maxConcurrent = 100
rate          = 1000.0
)

serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
if err != nil {
t.Fatalf("ListenUDP() error = %v", err)
}
defer serverConn.Close()

var inviteCount atomic.Int64
done := make(chan struct{})
go func() {
defer close(done)
buffer := make([]byte, 65535)
for {
_ = serverConn.SetReadDeadline(time.Now().Add(5 * time.Second))
n, addr, err := serverConn.ReadFromUDP(buffer)
if err != nil {
return
}
msg := sip.GetMessage()
if err := sip.ParseInto(msg, buffer[:n]); err != nil {
sip.PutMessage(msg)
continue
}
callID, _ := sip.Header(msg.Headers, "Call-ID")
from, _ := sip.Header(msg.Headers, "From")
to, _ := sip.Header(msg.Headers, "To")
via, _ := sip.Header(msg.Headers, "Via")
cseq, _ := sip.Header(msg.Headers, "CSeq")

switch strings.ToUpper(msg.Method) {
case "INVITE":
inviteCount.Add(1)
response := fmt.Sprintf(
"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContact: <sip:127.0.0.1:%d>\r\nContent-Length: 0\r\n\r\n",
via, from, to, callID, cseq, serverConn.LocalAddr().(*net.UDPAddr).Port,
)
_, _ = serverConn.WriteToUDP([]byte(response), addr)
case "ACK":
// ignore
case "BYE":
response := fmt.Sprintf(
"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\nContent-Length: 0\r\n\r\n",
via, from, to, callID, cseq,
)
_, _ = serverConn.WriteToUDP([]byte(response), addr)
}
sip.PutMessage(msg)
}
}()

sc, err := scenario.ParseFile("../../testdata/scenarios/basic_uac.xml")
if err != nil {
t.Fatalf("ParseFile() error = %v", err)
}


app := New(Config{
Scenario:      sc,
Transport:     "u1",
LocalIP:       "127.0.0.1",
RemoteHost:    "127.0.0.1",
RemotePort:    serverConn.LocalAddr().(*net.UDPAddr).Port,
Service:       "echo",
Rate:          rate,
TotalCalls:    totalCalls,
MaxConcurrent: maxConcurrent,
DefaultPause:  5 * time.Millisecond,
DefaultRecvTO: 2 * time.Second,
})

ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
defer cancel()

if err := app.Run(ctx); err != nil {
t.Fatalf("Run() error = %v", err)
}

summary := app.Stats().Snapshot()
t.Logf("Results: success=%d failed=%d invites_received=%d",
summary.SuccessCalls, summary.FailedCalls, inviteCount.Load())

if inviteCount.Load() == 0 {
t.Fatal("BUG: not a single INVITE was sent!")
}
if summary.FailedCalls > 0 {
t.Errorf("had %d failed calls (expected 0)", summary.FailedCalls)
}
}

// TestStressHighCPSWithLargeCSV verifies that a large injection CSV doesn't
// block call startup under high CPS (regression: csvRecordAt used to re-read
// the entire file on every [fieldN] reference).
func TestStressHighCPSWithLargeCSV(t *testing.T) {
const (
totalCalls    = 100
maxConcurrent = 50
rate          = 1000.0
csvRows       = 1000
)

// Create a large CSV injection file
csvDir := t.TempDir()
csvPath := csvDir + "/inject.csv"
{
f, err := os.Create(csvPath)
if err != nil {
t.Fatal(err)
}
_, _ = f.WriteString("SEQUENTIAL\n")
for i := 0; i < csvRows; i++ {
// ~200 bytes per row (simulates real JWT-laden CSV)
_, _ = fmt.Fprintf(f, "+336130%05d;uuid-%d;eyJhbGciOiJSUzI1NiJ9.%0100d;extra-%d\n", i, i, i, i)
}
f.Close()
}

// Simple scenario with [field0] reference
scenDir := t.TempDir()
scenPath := scenDir + "/uac_csv.xml"
{
scenXML := `<?xml version="1.0" encoding="UTF-8"?>
<scenario name="CSV UAC">
  <send retrans="200">
    <![CDATA[
INVITE sip:[field0]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:[field0]@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [field0] <sip:[field0]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
Contact: <sip:test@[local_ip]:[local_port]>
Content-Length: 0

]]>
  </send>
  <recv response="200"/>
  <send>
    <![CDATA[
ACK sip:[field0]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: test <sip:[field0]@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [field0] <sip:[field0]@[remote_ip]:[remote_port]>[peer_tag_param]
Call-ID: [call_id]
CSeq: 1 ACK
Contact: <sip:test@[local_ip]:[local_port]>
Content-Length: 0

]]>
  </send>
</scenario>`
if err := os.WriteFile(scenPath, []byte(scenXML), 0644); err != nil {
t.Fatal(err)
}
}

serverConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
if err != nil {
t.Fatal(err)
}
defer serverConn.Close()

var inviteCount atomic.Int64
go func() {
buffer := make([]byte, 65535)
for {
_ = serverConn.SetReadDeadline(time.Now().Add(5 * time.Second))
n, addr, err := serverConn.ReadFromUDP(buffer)
if err != nil {
return
}
msg := sip.GetMessage()
if err := sip.ParseInto(msg, buffer[:n]); err != nil {
sip.PutMessage(msg)
continue
}
callID, _ := sip.Header(msg.Headers, "Call-ID")
from, _ := sip.Header(msg.Headers, "From")
to, _ := sip.Header(msg.Headers, "To")
via, _ := sip.Header(msg.Headers, "Via")
cseq, _ := sip.Header(msg.Headers, "CSeq")

if strings.ToUpper(msg.Method) == "INVITE" {
inviteCount.Add(1)
response := fmt.Sprintf(
"SIP/2.0 200 OK\r\nVia: %s\r\nFrom: %s\r\nTo: %s;tag=peer\r\nCall-ID: %s\r\nCSeq: %s\r\nContact: <sip:127.0.0.1:%d>\r\nContent-Length: 0\r\n\r\n",
via, from, to, callID, cseq, serverConn.LocalAddr().(*net.UDPAddr).Port,
)
_, _ = serverConn.WriteToUDP([]byte(response), addr)
}
sip.PutMessage(msg)
}
}()

sc, err := scenario.ParseFile(scenPath)
if err != nil {
t.Fatal(err)
}

app := New(Config{
Scenario:      sc,
Transport:     "u1",
LocalIP:       "127.0.0.1",
RemoteHost:    "127.0.0.1",
RemotePort:    serverConn.LocalAddr().(*net.UDPAddr).Port,
Service:       "echo",
Rate:          rate,
TotalCalls:    totalCalls,
MaxConcurrent: maxConcurrent,
DefaultPause:  5 * time.Millisecond,
DefaultRecvTO: 2 * time.Second,
InjectionFile: csvPath,
})

ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

start := time.Now()
if err := app.Run(ctx); err != nil {
t.Fatalf("Run() error = %v", err)
}
elapsed := time.Since(start)

t.Logf("Results: success=%d invites=%d elapsed=%v",
app.Stats().Snapshot().SuccessCalls, inviteCount.Load(), elapsed)

if inviteCount.Load() < int64(totalCalls) {
t.Fatalf("expected %d INVITEs, got %d", totalCalls, inviteCount.Load())
}
// With caching, 100 calls at 1000 CPS should complete well under 2s
if elapsed > 5*time.Second {
t.Fatalf("took too long: %v (CSV caching may not be working)", elapsed)
}
}
