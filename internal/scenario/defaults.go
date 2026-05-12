package scenario

const defaultUAC = `<?xml version="1.0" encoding="UTF-8"?>
<scenario name="Basic Gossip UAC">
  <send retrans="500">
    <![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: gossip <sip:gossip@[local_ip]:[local_port]>;tag=[pid]GossipTag00[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
Contact: <sip:gossip@[local_ip]:[local_port]>
Max-Forwards: 70
Content-Length: 0

]]>
  </send>
  <recv response="100" optional="true"/>
  <recv response="180" optional="true"/>
  <recv response="200"/>
  <send>
    <![CDATA[
ACK sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: gossip <sip:gossip@[local_ip]:[local_port]>;tag=[pid]GossipTag00[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>[peer_tag_param]
Call-ID: [call_id]
CSeq: 1 ACK
Contact: <sip:gossip@[local_ip]:[local_port]>
Max-Forwards: 70
Content-Length: 0

]]>
  </send>
  <pause milliseconds="1000"/>
  <send retrans="500">
    <![CDATA[
BYE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: gossip <sip:gossip@[local_ip]:[local_port]>;tag=[pid]GossipTag00[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>[peer_tag_param]
Call-ID: [call_id]
CSeq: 2 BYE
Contact: <sip:gossip@[local_ip]:[local_port]>
Max-Forwards: 70
Content-Length: 0

]]>
  </send>
  <recv response="200"/>
</scenario>`

const defaultUAS = `<?xml version="1.0" encoding="UTF-8"?>
<scenario name="Basic Gossip UAS">
  <recv request="INVITE"/>
  <send>
    <![CDATA[
SIP/2.0 180 Ringing
[last_Via:]
[last_From:]
[last_To:];tag=[pid]GossipTag01[call_number]
[last_Call-ID:]
[last_CSeq:]
Contact: <sip:[local_ip]:[local_port];transport=[transport]>
Content-Length: 0

]]>
  </send>
  <send retrans="500">
    <![CDATA[
SIP/2.0 200 OK
[last_Via:]
[last_From:]
[last_To:];tag=[pid]GossipTag01[call_number]
[last_Call-ID:]
[last_CSeq:]
Contact: <sip:[local_ip]:[local_port];transport=[transport]>
Content-Length: 0

]]>
  </send>
  <recv request="ACK" optional="true"/>
  <recv request="BYE"/>
  <send>
    <![CDATA[
SIP/2.0 200 OK
[last_Via:]
[last_From:]
[last_To:]
[last_Call-ID:]
[last_CSeq:]
Contact: <sip:[local_ip]:[local_port];transport=[transport]>
Content-Length: 0

]]>
  </send>
  <timewait milliseconds="1000"/>
</scenario>`

const defaultInviteMedia = `<?xml version="1.0" encoding="UTF-8"?>
<scenario name="invite_media">
  <send retrans="500">
    <![CDATA[
INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: gossip <sip:gossip@[local_ip]:[local_port]>;tag=[pid]InvMedia[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 INVITE
Contact: <sip:gossip@[local_ip]:[local_port]>
Max-Forwards: 70
Content-Type: application/sdp
Content-Length: [len]

v=0
o=gossip 1 1 IN IP4 [local_ip]
s=-
c=IN IP4 [local_ip]
t=0 0
m=audio [media_port] RTP/AVP 0
a=rtpmap:0 PCMU/8000
]]>
  </send>

  <recv response="100" optional="true"/>
  <recv response="180" optional="true"/>
  <recv response="183" optional="true"/>
  <recv response="200">
    <action>
      <exec rtp_stream="synthetic,,0,PCMU/8000,20,3000"/>
    </action>
  </recv>

  <send>
    <![CDATA[
ACK sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: gossip <sip:gossip@[local_ip]:[local_port]>;tag=[pid]InvMedia[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>[peer_tag_param]
Call-ID: [call_id]
CSeq: 1 ACK
Max-Forwards: 70
Content-Length: 0

]]>
  </send>

  <pause milliseconds="500"/>

  <nop>
    <action>
      <exec rtp_stream="stop"/>
    </action>
  </nop>

  <send retrans="500">
    <![CDATA[
BYE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: gossip <sip:gossip@[local_ip]:[local_port]>;tag=[pid]InvMedia[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>[peer_tag_param]
Call-ID: [call_id]
CSeq: 2 BYE
Max-Forwards: 70
Content-Length: 0

]]>
  </send>

  <recv response="200"/>
</scenario>`

func LoadNamed(name string) (Scenario, error) {
	switch name {
	case "", "uac":
		sc, err := ParseString(defaultUAC)
		sc.BasePath = "."
		return sc, err
	case "uas":
		sc, err := ParseString(defaultUAS)
		sc.BasePath = "."
		return sc, err
	case "invite_media":
		sc, err := ParseString(defaultInviteMedia)
		sc.BasePath = "."
		return sc, err
	default:
		return Scenario{}, ErrUnknownScenario(name)
	}
}
