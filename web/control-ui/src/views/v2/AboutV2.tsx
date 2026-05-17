import { useEffect, useState } from 'react'

import { getHealthV2, type HealthV2 } from '@/api/v2'

const REPO_URL = 'https://github.com/sipcapture/gossipper'
const HOMER_URL = 'https://github.com/sipcapture/homer'

const FEATURES: { title: string; body: string }[] = [
  {
    title: 'SIP load + UAS engine',
    body: 'UAC / UAS / management profiles over UDP, TCP, TLS and WebSocket (plain or secure). Multiple listeners per profile, pluggable scenarios, per-call media (PCMU/PCMA), SRTP via SDES.',
  },
  {
    title: 'Admin console (api v2)',
    body: 'This UI talks to /api/v2/* — JSON profiles in profiles/servers, profiles/clients, scenarios in scenarios/, WAV / PCAP in media/, jobs and audit log in settings.sqlite.',
  },
  {
    title: 'Supervisor + jobs',
    body: 'The master process spawns each Start as an isolated `gossipper worker --spec` child, captures stdout/stderr as artifacts, and exposes lifecycle + recordings to the UI.',
  },
  {
    title: 'Observability',
    body: 'Per-job WAV recording, summary.json, stats.jsonl, HEP forwarding (HOMER), optional OTLP event export — all wired through the Jobs page.',
  },
  {
    title: 'Auth & audit',
    body: 'Optional internal SQLite + JWT users (role admin/user). Every mutating call is written to the audit_log table and surfaced under Settings → Audit.',
  },
]

export type AboutV2Props = {
  bearer?: string
}

export function AboutV2({ bearer }: AboutV2Props) {
  const [health, setHealth] = useState<HealthV2 | null>(null)
  const [err, setErr] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const h = await getHealthV2({ bearer })
        if (!cancelled) setHealth(h)
      } catch (e) {
        if (!cancelled) setErr(e instanceof Error ? e.message : String(e))
      }
    })()
    return () => {
      cancelled = true
    }
  }, [bearer])

  return (
    <section className="flex max-w-3xl flex-col gap-4">
      <header className="flex flex-col gap-1">
        <h2 className="text-base font-semibold tracking-tight">About Gossipper</h2>
        <p className="text-muted-foreground text-xs leading-relaxed">
          Gossipper is a high-performance SIP traffic generator and call simulator built for
          load-testing, scenario replay, NOC drills and integration tests. It speaks SIP over
          UDP / TCP / TLS / WS / WSS, simulates real RTP media flows, and reports everything
          through this admin console and via HEP into{' '}
          <a
            href={HOMER_URL}
            target="_blank"
            rel="noreferrer noopener"
            className="text-primary underline-offset-2 hover:underline"
          >
            HOMER
          </a>
          .
        </p>
      </header>

      <div className="border-border bg-card grid grid-cols-1 gap-2 rounded-md border p-3 sm:grid-cols-2">
        <Kv k="Version" v={health?.version ?? '…'} />
        <Kv k="Auth mode" v={health?.auth ?? '…'} />
        <Kv k="API" v="/api/v2/* (admin console)" />
        <Kv k="Status" v={err ? `error: ${err}` : health?.status ?? '…'} />
      </div>

      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
        {FEATURES.map((f) => (
          <article
            key={f.title}
            className="border-border bg-card flex flex-col gap-1 rounded-md border p-3"
          >
            <h3 className="text-sm font-medium">{f.title}</h3>
            <p className="text-muted-foreground text-xs leading-relaxed">{f.body}</p>
          </article>
        ))}
      </div>

      <footer className="text-muted-foreground flex flex-wrap items-center gap-3 text-[11px]">
        <a
          href={REPO_URL}
          target="_blank"
          rel="noreferrer noopener"
          className="text-primary underline-offset-2 hover:underline"
        >
          github.com/sipcapture/gossipper
        </a>
        <span>·</span>
        <a
          href={`${REPO_URL}/blob/main/CHANGELOG.md`}
          target="_blank"
          rel="noreferrer noopener"
          className="text-primary underline-offset-2 hover:underline"
        >
          changelog
        </a>
        <span>·</span>
        <a
          href={`${REPO_URL}/tree/main/docs`}
          target="_blank"
          rel="noreferrer noopener"
          className="text-primary underline-offset-2 hover:underline"
        >
          docs
        </a>
        <span>·</span>
        <span>Apache-2.0</span>
      </footer>
    </section>
  )
}

function Kv({ k, v }: { k: string; v: string }) {
  return (
    <div className="flex items-baseline justify-between gap-2 text-xs">
      <span className="text-muted-foreground">{k}</span>
      <code className="text-foreground/90 text-[11px]">{v}</code>
    </div>
  )
}
