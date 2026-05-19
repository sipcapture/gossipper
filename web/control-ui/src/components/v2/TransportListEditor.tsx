import { useCallback } from 'react'

import type { TransportSpec } from '@/api/v2'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { webrtcMissingICE } from '@/lib/profileHelpers'

const TRANSPORT_OPTIONS: { value: string; label: string; beta?: boolean }[] = [
  { value: 'u1', label: 'u1 — UDP (shared)' },
  { value: 'un', label: 'un — UDP (per-call)' },
  { value: 't1', label: 't1 — TCP (shared)' },
  { value: 'tn', label: 'tn — TCP (per-call)' },
  { value: 'l1', label: 'l1 — TLS (shared)' },
  { value: 'ln', label: 'ln — TLS (per-call)' },
  { value: 'w1', label: 'w1 — WebSocket (shared)' },
  { value: 'wn', label: 'wn — WebSocket (per-call)' },
  { value: 'ws1', label: 'ws1 — WSS (shared)' },
  { value: 'wsn', label: 'wsn — WSS (per-call)' },
  { value: 'webrtc', label: 'webrtc — WebRTC media (experimental)', beta: true },
]

export type TransportListEditorProps = {
  value: TransportSpec[]
  onChange: (next: TransportSpec[]) => void
  defaultPort?: number
}

export function TransportListEditor({ value, onChange, defaultPort = 5060 }: TransportListEditorProps) {
  const missingIce = webrtcMissingICE(value)
  const update = useCallback(
    (idx: number, patch: Partial<TransportSpec>) => {
      const next = value.slice()
      next[idx] = { ...next[idx], ...patch }
      onChange(next)
    },
    [value, onChange],
  )

  const remove = useCallback(
    (idx: number) => {
      onChange(value.filter((_, i) => i !== idx))
    },
    [value, onChange],
  )

  const add = useCallback(() => {
    onChange([
      ...value,
      {
        transport: 'u1',
        local_ip: '0.0.0.0',
        local_port: defaultPort + value.length,
        enabled: true,
      },
    ])
  }, [value, onChange, defaultPort])

  return (
    <div className="flex flex-col gap-2">
      {missingIce ? (
        <p className="border-warning/40 bg-warning/10 text-warning rounded-md border px-2 py-1.5 text-[11px]">
          WebRTC transport enabled without ICE servers — add STUN and/or TURN URLs for NAT traversal.
        </p>
      ) : null}
      {value.length === 0 ? (
        <p className="text-muted-foreground text-xs">No transports yet. Add the first one below.</p>
      ) : null}
      {value.map((t, idx) => (
        <div
          key={idx}
          className="border-border rounded-md border bg-muted/20 p-2"
        >
          <div className="grid grid-cols-1 gap-2 md:grid-cols-12">
            <div className="md:col-span-4">
              <Label className="text-[10px]">Transport</Label>
              <select
                value={t.transport}
                onChange={(e) => update(idx, { transport: e.target.value })}
                className="border-input bg-background mt-1 w-full rounded-md border px-2 py-1.5 text-sm"
              >
                {TRANSPORT_OPTIONS.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                    {o.beta ? ' (beta)' : ''}
                  </option>
                ))}
              </select>
            </div>
            <div className="md:col-span-4">
              <Label className="text-[10px]">Local IP</Label>
              <Input
                value={t.local_ip ?? ''}
                onChange={(e) => update(idx, { local_ip: e.target.value })}
                placeholder="0.0.0.0"
                className="mt-1"
              />
            </div>
            <div className="md:col-span-2">
              <Label className="text-[10px]">Port</Label>
              <Input
                type="number"
                value={t.local_port ?? 0}
                onChange={(e) => update(idx, { local_port: Number(e.target.value) || 0 })}
                className="mt-1"
              />
            </div>
            <div className="flex items-end gap-2 md:col-span-2">
              <label className="flex items-center gap-1 text-xs">
                <input
                  type="checkbox"
                  checked={t.enabled}
                  onChange={(e) => update(idx, { enabled: e.target.checked })}
                />
                enabled
              </label>
            </div>
          </div>
          {(t.transport === 'l1' || t.transport === 'ln' || t.transport === 'ws1' || t.transport === 'wsn') && (
            <div className="mt-2 grid grid-cols-1 gap-2 md:grid-cols-12">
              <div className="md:col-span-6">
                <Label className="text-[10px]">TLS cert file</Label>
                <Input
                  value={t.tls_cert_file ?? ''}
                  onChange={(e) => update(idx, { tls_cert_file: e.target.value })}
                  placeholder="/etc/ssl/private/sip.pem"
                  className="mt-1"
                />
              </div>
              <div className="md:col-span-6">
                <Label className="text-[10px]">TLS key file</Label>
                <Input
                  value={t.tls_key_file ?? ''}
                  onChange={(e) => update(idx, { tls_key_file: e.target.value })}
                  placeholder="/etc/ssl/private/sip.key"
                  className="mt-1"
                />
              </div>
            </div>
          )}
          {(t.transport === 'w1' || t.transport === 'wn' || t.transport === 'ws1' || t.transport === 'wsn') && (
            <div className="mt-2">
              <Label className="text-[10px]">WebSocket path</Label>
              <Input
                value={t.ws_path ?? ''}
                onChange={(e) => update(idx, { ws_path: e.target.value })}
                placeholder="/sip"
                className="mt-1"
              />
            </div>
          )}
          {t.transport === 'webrtc' && (
            <div className="mt-2 grid grid-cols-1 gap-2 md:grid-cols-12">
              <div className="md:col-span-12">
                <Label className="text-[10px]">
                  ICE servers (one URL per line — stun:host:port, turn:host:port, or turn:user:pass@host:port)
                </Label>
                <textarea
                  value={(t.ice_servers ?? []).join('\n')}
                  onChange={(e) =>
                    update(idx, {
                      ice_servers: e.target.value
                        .split(/\r?\n/)
                        .map((s) => s.trim())
                        .filter((s) => s.length > 0),
                    })
                  }
                  rows={3}
                  placeholder={'stun:stun.l.google.com:19302\nturn:turn.example.com:3478?transport=udp'}
                  className="border-input bg-background mt-1 w-full rounded-md border px-2 py-1.5 font-mono text-xs"
                />
                <div className="mt-1 flex flex-wrap gap-1">
                  <Button
                    type="button"
                    size="xs"
                    variant="outline"
                    onClick={() =>
                      update(idx, {
                        ice_servers: ['stun:stun.l.google.com:19302'],
                      })
                    }
                  >
                    + Google STUN
                  </Button>
                </div>
              </div>
              <div className="md:col-span-4">
                <Label className="text-[10px]">TURN username (static or REST identity)</Label>
                <Input
                  value={t.ice_username ?? ''}
                  onChange={(e) => update(idx, { ice_username: e.target.value })}
                  className="mt-1"
                />
              </div>
              <div className="md:col-span-4">
                <Label className="text-[10px]">TURN credential (static)</Label>
                <Input
                  value={t.ice_credential ?? ''}
                  onChange={(e) => update(idx, { ice_credential: e.target.value })}
                  className="mt-1"
                />
              </div>
              <div className="md:col-span-4">
                <Label className="text-[10px]">TURN REST secret (coturn)</Label>
                <Input
                  type="password"
                  value={t.ice_auth_secret ?? ''}
                  onChange={(e) => update(idx, { ice_auth_secret: e.target.value })}
                  placeholder="use-auth-secret"
                  className="mt-1"
                />
              </div>
              <div className="md:col-span-3">
                <Label className="text-[10px]">REST TTL (sec)</Label>
                <Input
                  type="number"
                  value={t.ice_auth_ttl_sec ?? 86400}
                  onChange={(e) => update(idx, { ice_auth_ttl_sec: Number(e.target.value) || 86400 })}
                  className="mt-1"
                />
              </div>
              <div className="flex items-end md:col-span-2">
                <label className="text-xs flex items-center gap-1">
                  <input
                    type="checkbox"
                    checked={t.prefers_pcma ?? false}
                    onChange={(e) => update(idx, { prefers_pcma: e.target.checked })}
                  />
                  prefer PCMA
                </label>
              </div>
            </div>
          )}
          <div className="mt-2 flex justify-end">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => remove(idx)}
              className="text-xs"
            >
              Remove
            </Button>
          </div>
        </div>
      ))}
      <Button type="button" variant="outline" size="sm" onClick={add} className="w-fit text-xs">
        + Add transport
      </Button>
    </div>
  )
}
