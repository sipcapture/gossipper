import { useCallback, useMemo, useState } from 'react'

import type { SIPTraceMessage } from '@/api/v2'
import { Button } from '@/components/ui/button'
import { Modal } from '@/components/ui/modal'
import { highlightSIP } from '@/lib/sipHighlight'

export function formatTraceTime(ts: string): string {
  const d = new Date(ts)
  if (Number.isNaN(d.getTime())) return ts
  return d.toISOString().slice(11, 23)
}

export function SipMessageModal({ msg, onClose }: { msg: SIPTraceMessage | null; onClose: () => void }) {
  if (!msg) return null
  return <SipMessageModalOpen key={msg.seq} msg={msg} onClose={onClose} />
}

function SipMessageModalOpen({ msg, onClose }: { msg: SIPTraceMessage; onClose: () => void }) {
  const [copied, setCopied] = useState(false)
  const raw = msg.raw || msg.summary || ''
  const html = useMemo(() => highlightSIP(raw), [raw])

  const onCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(raw)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      /* clipboard may be denied */
    }
  }, [raw])

  const app = msg.kind === 'app'
  const dir = app ? 'DBG' : msg.dir === 'send' ? 'OUT' : 'IN'
  const bits = [dir, formatTraceTime(msg.ts), msg.peer, msg.call_id].filter(Boolean)

  return (
    <Modal
      open
      onClose={onClose}
      size="xl"
      title={msg.summary || 'SIP message'}
      description={bits.join(' · ')}
      footer={
        <>
          <Button type="button" size="sm" variant="outline" onClick={() => void onCopy()}>
            {copied ? 'Copied' : 'Copy'}
          </Button>
          <Button type="button" size="sm" variant="outline" onClick={onClose}>
            Close
          </Button>
        </>
      }
    >
      <pre
        className="bg-muted/30 max-h-[min(70vh,36rem)] overflow-auto rounded-md border p-3 font-mono text-[12px] leading-relaxed whitespace-pre"
        dangerouslySetInnerHTML={{ __html: html }}
      />
    </Modal>
  )
}
