/** Admin-console nav targets for stress-tool shortcuts. */
export type StressToolNavTarget =
  | 'dashboard'
  | 'servers'
  | 'clients'
  | 'scenarios'
  | 'jobs'
  | 'media'
  | 'users'

export type StressToolStatus = 'ui' | 'cli' | 'both' | 'api'

export type StressToolEntry = {
  id: string
  title: string
  summary: string
  /** Example CLI invocation (sipstress-era utilities still run on the host). */
  cli?: string
  /** Path under repo docs/ (linked from About). */
  docsPath?: string
  uiNav?: StressToolNavTarget
  status: StressToolStatus
  /** When set, tool can be submitted as a supervisor job via POST /api/v2/tools/{id}/run */
  apiToolId?: string
}

export type StressToolGroup = {
  id: string
  title: string
  description: string
  tools: StressToolEntry[]
}

const DOCS = 'https://github.com/sipcapture/gossipper/blob/main/docs'

/** Catalog of sipstress-style capabilities integrated into Gossipper. */
export const STRESS_TOOL_GROUPS: StressToolGroup[] = [
  {
    id: 'load',
    title: 'Load & soak',
    description:
      'Long-run SIP stress, hybrid management + UAC engines, and supervisor jobs — the core sipstress-style control plane.',
    tools: [
      {
        id: 'server-hybrid',
        title: 'Management + hybrid JSON',
        summary: 'Long-run UAS with HTTP API; optional clients[] in one process for multi-engine soak.',
        cli: 'gossipper server -config /path/to/gossipper-hybrid.json',
        docsPath: `${DOCS}/sipstress-style-load-testing.md`,
        uiNav: 'servers',
        status: 'both',
      },
      {
        id: 'soak-unlimited',
        title: 'Unlimited soak (-m 0)',
        summary: 'Open-ended stress until SIGINT or global timeout. Set total_calls: 0 in client JSON or -m 0 on CLI.',
        cli: 'gossipper sipp -sn uac -rsa 127.0.0.1:5060 -m 0 -r 50',
        docsPath: `${DOCS}/run-profile.md`,
        uiNav: 'clients',
        status: 'both',
      },
      {
        id: 'jobs-supervisor',
        title: 'Supervisor jobs',
        summary: 'Start/stop isolated workers from UAS/UAC profiles; artifacts (stats, WAV, summary) under artifacts/jobs/.',
        docsPath: `${DOCS}/ui-mode.md`,
        uiNav: 'jobs',
        status: 'ui',
      },
      {
        id: 'dynamic-clients',
        title: 'Dynamic UAC clients (API v1)',
        summary: 'POST /api/v1/clients at runtime when management server exposes legacy v1 alongside v2.',
        cli: 'curl -X POST http://127.0.0.1:8080/api/v1/clients -H "Authorization: Bearer …" -d @client-snippet.json',
        docsPath: `${DOCS}/sipstress-style-load-testing.md`,
        uiNav: 'clients',
        status: 'both',
      },
    ],
  },
  {
    id: 'prep',
    title: 'Scenario prep',
    description: 'Turn captures and CSV data into runnable XML before loading them in Scenarios or Jobs.',
    tools: [
      {
        id: 'pcap2scenario',
        title: 'pcap2scenario',
        summary: 'PCAP → paired UAC/UAS XML scenarios (SIP + RTP dialog reconstruction).',
        cli: 'gossipper pcap2scenario capture.pcap -out ./scenarios -sip-port 5060',
        docsPath: `${DOCS}/pcap2scenario.md`,
        uiNav: 'scenarios',
        status: 'both',
        apiToolId: 'pcap2scenario',
      },
      {
        id: 'infindex',
        title: 'CSV infindex (-infindex)',
        summary: 'Build a lookup index for SIPp-style CSV injection (accelerates [field0 … line=$var]).',
        cli: 'gossipper sipp -infindex ./users.csv 0',
        docsPath: `${DOCS}/compatibility.md`,
        status: 'both',
        apiToolId: 'infindex',
      },
    ],
  },
  {
    id: 'media-rtp',
    title: 'Media & RTP',
    description: 'Synthetic RTP and uploaded WAV/PCAP assets for media-heavy scenarios.',
    tools: [
      {
        id: 'rtp-send',
        title: 'Standalone RTP sender (-rtp_send)',
        summary: 'Synthetic G.711 streams without a SIP scenario — useful for media path validation.',
        cli: 'gossipper -rtp_send -rtp_addr 127.0.0.1:4000 -rtp_codec PCMU/8000 -m 1',
        docsPath: `${DOCS}/synthetic-rtp-sender.md`,
        status: 'both',
        apiToolId: 'rtp_send',
      },
      {
        id: 'media-library',
        title: 'Media library',
        summary: 'Upload WAV/PCAP and reference as [[media:wav/name]] in scenario XML.',
        docsPath: `${DOCS}/ui-mode.md`,
        uiNav: 'media',
        status: 'ui',
      },
    ],
  },
  {
    id: 'reports',
    title: 'Reports & export',
    description: 'Post-run summaries from job artifacts or CLI JSON export.',
    tools: [
      {
        id: 'summary-json',
        title: 'Engine summary JSON',
        summary: 'Per-run summary.json in job artifacts or via -summary_json on CLI runs.',
        cli: 'gossipper sipp -sn uac -rsa 127.0.0.1:5060 -m 10 -summary_json /tmp/summary.json',
        docsPath: `${DOCS}/summary-json.md`,
        uiNav: 'jobs',
        status: 'both',
      },
      {
        id: 'report-html',
        title: 'report-html',
        summary: 'Convert summary JSON to a standalone offline HTML report.',
        cli: 'gossipper report-html -in summary.json -out report.html',
        docsPath: `${DOCS}/summary-json.md`,
        status: 'both',
        apiToolId: 'report-html',
      },
      {
        id: 'summary-to-pdf',
        title: 'summary-to-pdf',
        summary: 'Render HTML report to PDF (embedded chromedp with -tags pdf, else Chromium in PATH).',
        cli: 'gossipper summary-to-pdf -in report.html -out report.pdf',
        docsPath: `${DOCS}/summary-json.md`,
        status: 'both',
        apiToolId: 'summary-to-pdf',
      },
    ],
  },
  {
    id: 'interactive',
    title: 'Interactive CLI',
    description: 'Terminal-first launchers — not embedded in the web UI; run on the gossipper host.',
    tools: [
      {
        id: 'tui',
        title: 'tui / -interactive',
        summary: 'Full-screen runtime control (rate, pause, screen dump) while a scenario runs.',
        cli: 'gossipper tui',
        docsPath: `${DOCS}/tui.md`,
        status: 'cli',
      },
      {
        id: 'shell',
        title: 'shell / cli',
        summary: 'Line-oriented shell: set flags, wizard, hint, run — scriptable REPL.',
        cli: 'gossipper shell',
        docsPath: `${DOCS}/interactive-shell.md`,
        status: 'cli',
      },
      {
        id: 'auth-user-add',
        title: 'auth user-add',
        summary: 'Bootstrap Control UI users (also seeded automatically on first start: admin / sipcapture).',
        cli: 'gossipper auth user-add -config gossipper-server.json -username ops -password …',
        docsPath: `${DOCS}/cli.md`,
        uiNav: 'users',
        status: 'both',
      },
    ],
  },
]

export function stressToolStatusLabel(status: StressToolStatus): string {
  switch (status) {
    case 'ui':
      return 'In this UI'
    case 'cli':
      return 'CLI on host'
    case 'both':
      return 'UI + CLI'
    case 'api':
      return 'Job API'
  }
}
