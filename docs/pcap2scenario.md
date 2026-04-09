# pcap2scenario — генерация сценариев из PCAP

`gossipper pcap2scenario` читает PCAP-файл с SIP+RTP трафиком и создаёт пару
готовых к воспроизведению сценариев (UAC + UAS) вместе с извлечёнными
RTP-потоками.

---

## Быстрый старт

```bash
gossipper pcap2scenario call.pcap
```

Или с явными параметрами:

```bash
gossipper pcap2scenario call.pcap -out ./scenarios -sip-port 5060
```

---

## Флаги

| Флаг | По умолчанию | Описание |
|------|-------------|----------|
| `<file.pcap>` | — | Входной PCAP-файл (обязателен) |
| `-out <dir>` | `.` (текущая папка) | Директория для выходных файлов |
| `-sip-port <port>` | `0` (авто) | Порт SIP; `0` — эвристическое определение |

---

## Выходные файлы

После успешного запуска в выходной директории появятся:

| Файл | Описание |
|------|----------|
| `scenario_uac.xml` | Сценарий звонящего (client mode) |
| `scenario_uas.xml` | Сценарий принимающего (server mode) |
| `caller_rtp.pcap` | RTP-пакеты со стороны caller |
| `callee_rtp.pcap` | RTP-пакеты со стороны callee |

---

## Использование сценариев

### UAC (звонящий)

```bash
gossipper -scenario scenarios/scenario_uac.xml \
          -d 192.168.1.20 \
          -p 5060 \
          -l 192.168.1.10 \
          -r 1
```

### UAS (принимающий)

```bash
gossipper -scenario scenarios/scenario_uas.xml \
          -l 192.168.1.20 \
          -p 5060 \
          -r 1
```

> Файлы `caller_rtp.pcap` / `callee_rtp.pcap` должны лежать в той же директории,
> из которой запускается gossipper (или по которой он ищет ресурсы сценария).

---

## Поток обработки

```
call.pcap
    │
    ▼
┌─────────────────────────────────────────┐
│  Extractor                              │
│  • SIP по UDP  → sip.Parse()            │
│  • SIP по TCP  → tcpassembly +          │
│                  sip.ReadMessage()       │
│  • все UDP-пакеты (для RTP)             │
└───────────────┬─────────────────────────┘
                │
    ┌───────────▼──────────┐
    │  Dialog Builder      │
    │  INVITE → 200 → ACK  │
    │  BYE → 200           │
    │  SDP: RTP IP + порты │
    └───────────┬──────────┘
                │
    ┌───────────▼──────────────────────────────┐
    │  RTP Split                               │
    │  filter by callerRTPPort → caller_rtp.pcap│
    │  filter by calleeRTPPort → callee_rtp.pcap│
    └───────────┬──────────────────────────────┘
                │
    ┌───────────▼──────────────────────────────┐
    │  Generator                               │
    │  scenario_uac.xml  (темплатиз. SIP)      │
    │  scenario_uas.xml  (last_Via/From паттерн)│
    └──────────────────────────────────────────┘
```

---

## Темплатизация SIP-сообщений

Конкретные значения из PCAP заменяются на переменные gossipper:

| Поле в PCAP | Переменная в сценарии |
|---|---|
| IP caller | `[local_ip]` |
| IP callee | `[remote_ip]` |
| SIP-порт caller | `[local_port]` |
| SIP-порт callee | `[remote_port]` |
| Call-ID | `[call_id]` |
| Via branch | `[branch]` |
| Via транспорт | `[transport]` |
| From tag | `[pid]GossipTag00[call_number]` |
| To tag (ACK/BYE) | `[peer_tag_param]` |
| SDP `c=` адрес | `[local_ip]` |
| SDP `m=audio <port>` | `[media_port]` |
| Content-Length | `[len]` (пересчитывается) |
| CSeq номер | оставляется как есть |
| Прочие заголовки | оставляются как есть |

---

## Пример генерируемого UAC-сценария

```xml
<?xml version="1.0" encoding="UTF-8"?>
<scenario name="pcap-uac (from call.pcap)">

  <send retrans="500">
    <![CDATA[
    INVITE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
    Via: SIP/2.0/[transport] [local_ip]:[local_port];branch=[branch]
    From: "Alice" <sip:alice@[local_ip]:[local_port]>;tag=[pid]GossipTag00[call_number]
    To: "Bob" <sip:bob@[remote_ip]:[remote_port]>
    Call-ID: [call_id]
    CSeq: 1 INVITE
    Contact: <sip:alice@[local_ip]:[local_port];transport=[transport]>
    Content-Type: application/sdp
    Content-Length: [len]

    v=0
    o=- 0 0 IN IP4 [local_ip]
    s=-
    c=IN IP4 [local_ip]
    t=0 0
    m=audio [media_port] RTP/AVP 0 8
    a=rtpmap:0 PCMU/8000
    a=rtpmap:8 PCMA/8000
    a=sendrecv
    ]]>
  </send>

  <recv response="100" optional="true"/>
  <recv response="180" optional="true"/>

  <!-- На 200 OK сразу запускаем RTP-воспроизведение -->
  <recv response="200" rtd="true">
    <action>
      <exec play_pcap_audio="caller_rtp.pcap"/>
    </action>
  </recv>

  <send>
    <![CDATA[
    ACK sip:[service]@[remote_ip]:[remote_port] SIP/2.0
    ...
    ]]>
  </send>

  <!-- Пауза = длительность звонка из оригинального PCAP -->
  <pause milliseconds="5000"/>

  <nop>
    <action><exec rtp_stream="stop"/></action>
  </nop>

  <send retrans="500">
    <![CDATA[
    BYE sip:[service]@[remote_ip]:[remote_port] SIP/2.0
    ...
    ]]>
  </send>

  <recv response="200"/>

</scenario>
```

## Пример генерируемого UAS-сценария

```xml
<?xml version="1.0" encoding="UTF-8"?>
<scenario name="pcap-uas (from call.pcap)">

  <recv request="INVITE"/>

  <!-- 180 Ringing -->
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

  <!-- 200 OK с SDP (кодеки из оригинального PCAP) -->
  <send retrans="500">
    <![CDATA[
    SIP/2.0 200 OK
    [last_Via:]
    [last_From:]
    [last_To:];tag=[pid]GossipTag01[call_number]
    [last_Call-ID:]
    [last_CSeq:]
    Contact: <sip:[local_ip]:[local_port];transport=[transport]>
    Content-Type: application/sdp
    Content-Length: [len]

    v=0
    o=- 0 0 IN IP4 [local_ip]
    s=-
    c=IN IP4 [local_ip]
    t=0 0
    m=audio [media_port] RTP/AVP 0 8
    a=rtpmap:0 PCMU/8000
    a=rtpmap:8 PCMA/8000
    a=sendrecv
    ]]>
  </send>

  <!-- На ACK запускаем RTP со стороны callee -->
  <recv request="ACK">
    <action>
      <exec play_pcap_audio="callee_rtp.pcap"/>
    </action>
  </recv>

  <pause milliseconds="5000"/>

  <nop>
    <action><exec rtp_stream="stop"/></action>
  </nop>

  <recv request="BYE"/>

  <send>
    <![CDATA[
    SIP/2.0 200 OK
    [last_Via:]
    [last_From:]
    [last_To:];tag=[pid]GossipTag01[call_number]
    [last_Call-ID:]
    [last_CSeq:]
    Content-Length: 0
    ]]>
  </send>

</scenario>
```

---

## Поддерживаемые форматы

| Параметр | Поддержка |
|---|---|
| SIP over UDP | ✅ |
| SIP over TCP (с reassembly) | ✅ |
| RTP audio (`m=audio`) | ✅ |
| IPv4 | ✅ |
| Pcap (libpcap format) | ✅ |
| IPv6 | ⚠️ Парсируется, но темплатизация адресов не проверена |
| Несколько звонков в одном PCAP | ⚠️ Берётся первый найденный Call-ID |
| Re-INVITE / hold / transfer | ❌ v1 не поддерживает |
| Аутентификация 407/401 | ❌ v1 не поддерживает |
| SRTP | ❌ v1 не поддерживает |
| Видео RTP (`m=video`) | ❌ только audio |

---

## Как работает определение SIP

Если `-sip-port` не задан (значение `0`), SIP-датаграммы определяются
эвристически по началу payload:

```
INVITE   ACK   BYE   CANCEL   OPTIONS   REGISTER
NOTIFY   SUBSCRIBE   PUBLISH   REFER   INFO   UPDATE   PRACK
SIP/2.0 ...
```

Если SIP идёт на нестандартном порту, укажите его явно:

```bash
gossipper pcap2scenario call.pcap -sip-port 5080
```

---

## Связанная документация

- [RTP in scenarios](rtp-in-scenarios.md) — как использовать `play_pcap_audio` и `rtp_stream` в сценариях
- [Synthetic RTP sender](synthetic-rtp-sender.md) — генерация тишины без PCAP-файла
