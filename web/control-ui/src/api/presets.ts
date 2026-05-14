/** Minimal example scenarios (from gossipper testdata). */

export const PRESET_OPTIONS_CLIENT = `<?xml version="1.0" encoding="UTF-8"?>
<scenario name="options-client">
  <send><![CDATA[
OPTIONS sip:[service]@[remote_ip]:[remote_port] SIP/2.0
Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
From: probe <sip:probe@[local_ip]:[local_port]>;tag=[pid]Tag[call_number]
To: [service] <sip:[service]@[remote_ip]:[remote_port]>
Call-ID: [call_id]
CSeq: 1 OPTIONS
Contact: <sip:probe@[local_ip]:[local_port]>
Max-Forwards: 70
Content-Length: 0

]]></send>
  <recv response="200"/>
</scenario>
`

export const PRESET_OPTIONS_SERVER = `<?xml version="1.0" encoding="UTF-8"?>
<scenario name="options-server">
  <recv request="OPTIONS"/>
  <send><![CDATA[
SIP/2.0 200 OK
[last_Via:]
[last_From:]
[last_To:];tag=[pid]OptionsTag[call_number]
[last_Call-ID:]
[last_CSeq:]
Allow: OPTIONS
Content-Length: 0

]]></send>
</scenario>
`
