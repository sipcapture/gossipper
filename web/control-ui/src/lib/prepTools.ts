import type { ToolMeta } from '@/api/v2'

export type PrepToolEntry = {
  id: string
  title: string
  summary: string
  cli: string
  docsPath?: string
  apiToolId: string
  exampleArgs: Record<string, unknown>
}

const DOCS = 'https://github.com/sipcapture/gossipper/blob/main/docs'

/** Scenario-prep utilities (not load-test runners). Run as supervisor jobs or CLI. */
export const PREP_TOOLS: PrepToolEntry[] = [
  {
    id: 'pcap2scenario',
    title: 'pcap2scenario',
    summary: 'PCAP → paired UAC/UAS XML scenarios (SIP + RTP dialog reconstruction).',
    cli: 'gossipper pcap2scenario capture.pcap -out ./scenarios -sip-port 5060',
    docsPath: `${DOCS}/pcap2scenario.md`,
    apiToolId: 'pcap2scenario',
    exampleArgs: { pcap: 'media/pcap/capture.pcap', out: 'artifacts/jobs/<job-id>/scenarios', sip_port: 5060 },
  },
  {
    id: 'infindex',
    title: 'CSV infindex',
    summary: 'Build a lookup index for SIPp-style CSV injection files.',
    cli: 'gossipper sipp -infindex ./users.csv 0',
    docsPath: `${DOCS}/compatibility.md`,
    apiToolId: 'infindex',
    exampleArgs: { csv: 'media/inject/users.csv', field: 0 },
  },
  {
    id: 'rtp_send',
    title: 'rtp_send',
    summary: 'Synthetic RTP sender for media path validation.',
    cli: 'gossipper rtp_send -h',
    docsPath: `${DOCS}/synthetic-rtp-sender.md`,
    apiToolId: 'rtp_send',
    exampleArgs: { host: '127.0.0.1', port: 40000, duration_sec: 10 },
  },
  {
    id: 'report-html',
    title: 'report-html',
    summary: 'Render summary.json to static HTML report.',
    cli: 'gossipper report-html -in summary.json -out report.html',
    apiToolId: 'report-html',
    exampleArgs: { in: 'artifacts/jobs/<job-id>/summary.json', out: 'artifacts/jobs/<job-id>/report.html' },
  },
]

export function prepExampleArgs(meta: ToolMeta | undefined, entry: PrepToolEntry): string {
  const example = meta?.example_args ?? entry.exampleArgs
  return JSON.stringify(example, null, 2)
}
