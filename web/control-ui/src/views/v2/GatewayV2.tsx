import { useCallback, useEffect, useMemo, useRef, useState } from 'react'

import {
  armGatewayProfile,
  createGateway,
  deleteGateway,
  getGatewayIPs,
  listBuiltinScenarios,
  listGateways,
  listScenarios,
  originateGatewayProfile,
  updateGateway,
  type BuiltinScenarioMeta,
  type GatewayConfig,
  type GatewayIPHints,
  type GatewaySnapshot,
  type ScenarioMeta,
} from '@/api/v2'
import { Button } from '@/components/ui/button'
import { DataTable, type Column } from '@/components/ui/data-table'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Modal } from '@/components/ui/modal'
import { Switch } from '@/components/ui/switch'
import { ScenarioSelectField } from '@/components/v2/ScenarioSelect'
import {
  gatewayAOR,
  gatewayCanOriginate,
  gatewayContactPreview,
  gatewayLocalIPLabel,
  gatewaySaveBody,
  gatewaySaveOkText,
  gatewayStatusLabel,
} from '@/lib/gatewayView'
import type { NavId } from '@/lib/routing'

export type GatewayV2Props = {
  bearer?: string
  busy?: boolean
  run: <T>(fn: () => Promise<T>) => Promise<T | undefined>
  onNavigate?: (nav: NavId, opts?: { jobId?: string }) => void
}

const emptyCfg = (): GatewayConfig => ({
  id: '',
  name: '',
  enabled: true,
  domain: '',
  addr: '',
  username: '',
  password: '',
  register: true,
  register_user: '',
  advertised_ip: '',
  register_expires: 300,
  keepalive_seconds: 20,
  contact_port: 5060,
})

function formFromSnap(snap: GatewaySnapshot): GatewayConfig {
  return {
    ...emptyCfg(),
    ...snap.config,
    password: snap.config.password === '***' ? '' : (snap.config.password ?? ''),
  }
}

function origFromCfg(cfg: GatewayConfig): { origId: string; origTo: string; origCalls: number } {
  return {
    origId: cfg.originate_scenario_id?.trim() || 'one_way',
    origTo: cfg.originate_to ?? '',
    origCalls: cfg.originate_calls && cfg.originate_calls > 0 ? cfg.originate_calls : 1,
  }
}

export function GatewayV2({ bearer, busy, run, onNavigate }: GatewayV2Props) {
  const [rows, setRows] = useState<GatewaySnapshot[]>([])
  const [form, setForm] = useState<GatewayConfig | null>(null)
  const [createMode, setCreateMode] = useState(false)
  const [armId, setArmId] = useState('uas')
  const [origId, setOrigId] = useState('one_way')
  const [origTo, setOrigTo] = useState('')
  const [origCalls, setOrigCalls] = useState(1)
  const [scenarios, setScenarios] = useState<ScenarioMeta[]>([])
  const [builtins, setBuiltins] = useState<BuiltinScenarioMeta[]>([])
  const [unavailable, setUnavailable] = useState<string | null>(null)
  const [editSnap, setEditSnap] = useState<GatewaySnapshot | null>(null)
  const [saveNote, setSaveNote] = useState<{ kind: 'saving' | 'ok' | 'err' | 'edited'; text: string } | null>(
    null,
  )

  const refresh = useCallback(async () => {
    try {
      const next = await listGateways({ bearer })
      setUnavailable(null)
      setRows(next.gateways ?? [])
    } catch {
      setUnavailable(
        'Gateway API is not available on this process (needs gossipper server + management listen).',
      )
    }
  }, [bearer])

  const refreshRowsOnly = useCallback(async () => {
    try {
      const next = await listGateways({ bearer })
      setRows(next.gateways ?? [])
      setUnavailable(null)
    } catch (err) {
      console.warn('refresh gateways:', err)
    }
  }, [bearer])

  useEffect(() => {
    void run(() => refresh())
  }, [run, refresh])

  useEffect(() => {
    const id = window.setInterval(() => {
      void refreshRowsOnly()
    }, 3000)
    return () => window.clearInterval(id)
  }, [refreshRowsOnly])

  useEffect(() => {
    void Promise.all([
      listScenarios({ bearer }).catch(() => ({ scenarios: [] as ScenarioMeta[] })),
      listBuiltinScenarios({ bearer }).catch(() => ({ scenarios: [] as BuiltinScenarioMeta[] })),
    ]).then(([sc, bi]) => {
      setScenarios(sc.scenarios ?? [])
      setBuiltins(bi.scenarios ?? [])
    })
  }, [bearer])

  const onCreate = () => {
    setForm(emptyCfg())
    setCreateMode(true)
    setEditSnap(null)
    setSaveNote(null)
    setArmId('uas')
    setOrigId('one_way')
    setOrigTo('')
    setOrigCalls(1)
  }

  const onEdit = (row: GatewaySnapshot) => {
    setForm(formFromSnap(row))
    setCreateMode(false)
    setEditSnap(row)
    setSaveNote(null)
    setArmId(row.armed_scenario_id || 'uas')
    const orig = origFromCfg(row.config)
    setOrigId(orig.origId)
    setOrigTo(orig.origTo)
    setOrigCalls(orig.origCalls)
  }

  const onClose = () => {
    setForm(null)
    setEditSnap(null)
    setSaveNote(null)
  }

  const onFormChange = (next: GatewayConfig) => {
    setForm(next)
    setSaveNote((prev) => (prev?.kind === 'ok' ? { kind: 'edited', text: 'Unsaved changes' } : prev))
  }

  const onSave = () => {
    if (!form) return
    setSaveNote({ kind: 'saving', text: 'Saving…' })
    void (async () => {
      const out = await run(async () => {
        const body = gatewaySaveBody(form, { armId, origId, origTo, origCalls })
        if (createMode) {
          await createGateway(body, { bearer })
          await refresh()
          setForm(null)
          setEditSnap(null)
          setSaveNote(null)
          return 'created' as const
        }
        if (!form.id) return null
        const next = await updateGateway(form.id, body, { bearer })
        setEditSnap(next)
        setForm(formFromSnap(next))
        setArmId(next.armed_scenario_id || armId)
        const orig = origFromCfg(next.config)
        setOrigId(orig.origId)
        setOrigTo(orig.origTo)
        setOrigCalls(orig.origCalls)
        await refresh()
        return next
      })
      if (out === 'created') return
      if (out) {
        setSaveNote({ kind: 'ok', text: gatewaySaveOkText(out) })
        return
      }
      setSaveNote({ kind: 'err', text: 'Save failed' })
    })()
  }

  const onDelete = (row: GatewaySnapshot) => {
    const id = row.config.id
    if (!id) return
    const label = row.config.name || id
    if (!window.confirm(`Delete gateway profile "${label}"?`)) return
    void run(async () => {
      await deleteGateway(id, { bearer })
      await refresh()
    })
  }

  const onToggle = (row: GatewaySnapshot, enabled: boolean) => {
    const id = row.config.id
    if (!id) return
    void run(async () => {
      await updateGateway(
        id,
        gatewaySaveBody(
          { ...row.config, enabled, password: '' },
          {
            armId: row.armed_scenario_id || 'uas',
            origId: row.config.originate_scenario_id,
            origTo: row.config.originate_to,
            origCalls: row.config.originate_calls,
          },
        ),
        { bearer },
      )
      await refresh()
    })
  }

  const onArm = () => {
    const id = form?.id
    if (!id) return
    void run(async () => {
      const next = await armGatewayProfile(id, armId, { bearer })
      setEditSnap(next)
      setArmId(next.armed_scenario_id || armId)
      await refresh()
      return next
    })
  }

  const onOriginate = () => {
    const id = form?.id
    if (!id) return
    void run(async () => {
      const out = await originateGatewayProfile(
        id,
        { scenario_id: origId, to: origTo, total_calls: origCalls },
        { bearer },
      )
      if (out.job_id) onNavigate?.('jobs', { jobId: out.job_id })
      await refresh()
      return out
    })
  }

  const columns: Column<GatewaySnapshot>[] = useMemo(
    () => [
      {
        key: 'name',
        header: 'Name',
        render: (r) => (
          <span className="inline-flex flex-col">
            <span>{r.config.name || r.config.id || 'Gateway'}</span>
            {r.config.id ? <code className="text-muted-foreground text-[10px]">{r.config.id}</code> : null}
          </span>
        ),
      },
      {
        key: 'aor',
        header: 'AOR',
        render: (r) => <code className="text-xs">{gatewayAOR(r.config) || '—'}</code>,
      },
      {
        key: 'status',
        header: 'Status',
        render: (r) => {
          const state = r.status.state ?? 'off'
          return (
            <span
              className={
                state === 'registered'
                  ? 'text-emerald-600'
                  : state === 'failed'
                    ? 'text-destructive'
                    : 'text-muted-foreground'
              }
            >
              {gatewayStatusLabel(state)}
              {r.status.code ? ` · SIP ${r.status.code}` : ''}
            </span>
          )
        },
      },
      {
        key: 'enabled',
        header: 'On',
        render: (r) => (
          <Switch
            size="sm"
            checked={r.config.enabled !== false}
            disabled={busy || !r.config.id}
            onCheckedChange={(v) => onToggle(r, v)}
            aria-label={`Enable ${r.config.name || r.config.id || 'gateway'}`}
          />
        ),
      },
      {
        key: 'actions',
        header: '',
        align: 'right',
        render: (r) => (
          <div className="flex justify-end gap-1">
            <Button type="button" variant="outline" size="xs" onClick={() => onEdit(r)}>
              Edit
            </Button>
            <Button type="button" variant="destructive" size="xs" onClick={() => onDelete(r)}>
              Delete
            </Button>
          </div>
        ),
      },
    ],
    [busy],
  )

  const canOrig = form ? gatewayCanOriginate(form) : false

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 className="text-sm font-semibold">Gateway profiles</h2>
          <p className="text-muted-foreground text-xs">
            SIP REGISTER AORs. Disable a profile to stop REGISTER and originate. Inbound INVITE is
            matched on Request-URI user.
          </p>
        </div>
        <div className="flex gap-2">
          <Button type="button" variant="outline" size="sm" onClick={() => onNavigate?.('sip')}>
            Live Trace
          </Button>
          <Button type="button" variant="outline" size="sm" onClick={() => void run(() => refresh())} disabled={busy}>
            Refresh
          </Button>
          <Button type="button" size="sm" onClick={onCreate}>
            + New gateway
          </Button>
        </div>
      </div>

      {unavailable ? <p className="text-muted-foreground text-xs">{unavailable}</p> : null}

      <DataTable
        rows={rows}
        columns={columns}
        rowKey={(r) => r.config.id || r.config.username || r.config.name || 'row'}
        loading={Boolean(busy && rows.length === 0)}
        empty="No gateway profiles yet — create one above."
      />

      <Modal
        open={form !== null}
        onClose={onClose}
        size="lg"
        title={createMode ? 'New gateway profile' : `Edit gateway · ${form?.name || form?.id || ''}`}
        footer={
          <>
            <p className="mr-auto min-w-0 pr-3 text-[11px] leading-snug" role="status" aria-live="polite">
              {saveNote ? (
                <span
                  className={
                    saveNote.kind === 'ok'
                      ? 'text-emerald-600'
                      : saveNote.kind === 'err'
                        ? 'text-destructive'
                        : saveNote.kind === 'saving'
                          ? 'text-amber-600'
                          : 'text-muted-foreground'
                  }
                >
                  {saveNote.text}
                </span>
              ) : (
                <span className="text-muted-foreground">
                  {createMode
                    ? 'Create writes this profile and the selected UAS.'
                    : 'Save writes SIP fields, the selected UAS, and UAC originate defaults.'}
                </span>
              )}
            </p>
            <Button type="button" variant="outline" size="sm" onClick={onClose}>
              Cancel
            </Button>
            <Button type="button" size="sm" onClick={onSave} disabled={busy}>
              {saveNote?.kind === 'saving' ? 'Saving…' : createMode ? 'Create' : 'Save'}
            </Button>
          </>
        }
      >
        {form ? (
          <GatewayForm
            value={form}
            onChange={onFormChange}
            snap={editSnap}
            createMode={createMode}
            busy={Boolean(busy)}
            bearer={bearer}
            armId={armId}
            onArmId={(id) => {
              setArmId(id)
              setSaveNote((prev) => (prev?.kind === 'ok' ? { kind: 'edited', text: 'Unsaved changes' } : prev))
            }}
            origId={origId}
            onOrigId={(id) => {
              setOrigId(id)
              setSaveNote((prev) => (prev?.kind === 'ok' ? { kind: 'edited', text: 'Unsaved changes' } : prev))
            }}
            origTo={origTo}
            onOrigTo={(v) => {
              setOrigTo(v)
              setSaveNote((prev) => (prev?.kind === 'ok' ? { kind: 'edited', text: 'Unsaved changes' } : prev))
            }}
            origCalls={origCalls}
            onOrigCalls={(n) => {
              setOrigCalls(n)
              setSaveNote((prev) => (prev?.kind === 'ok' ? { kind: 'edited', text: 'Unsaved changes' } : prev))
            }}
            scenarios={scenarios}
            builtins={builtins}
            canOrig={canOrig}
            onArm={onArm}
            onOriginate={onOriginate}
          />
        ) : null}
      </Modal>
    </section>
  )
}

type GatewayFormProps = {
  value: GatewayConfig
  onChange: (next: GatewayConfig) => void
  snap: GatewaySnapshot | null
  createMode: boolean
  busy: boolean
  bearer?: string
  armId: string
  onArmId: (id: string) => void
  origId: string
  onOrigId: (id: string) => void
  origTo: string
  onOrigTo: (v: string) => void
  origCalls: number
  onOrigCalls: (n: number) => void
  scenarios: ScenarioMeta[]
  builtins: BuiltinScenarioMeta[]
  canOrig: boolean
  onArm: () => void
  onOriginate: () => void
}

function GatewayForm({
  value,
  onChange,
  snap,
  createMode,
  busy,
  bearer,
  armId,
  onArmId,
  origId,
  onOrigId,
  origTo,
  onOrigTo,
  origCalls,
  onOrigCalls,
  scenarios,
  builtins,
  canOrig,
  onArm,
  onOriginate,
}: GatewayFormProps) {
  const setField = <K extends keyof GatewayConfig>(k: K, v: GatewayConfig[K]) => {
    onChange({ ...value, [k]: v })
  }
  const state = snap?.status.state ?? 'off'

  return (
    <div className="flex flex-col gap-4 overflow-visible">
      <div className="flex flex-wrap items-center gap-3 text-xs">
        <label className="flex items-center gap-2">
          <Switch
            size="sm"
            checked={value.enabled !== false}
            onCheckedChange={(v) => setField('enabled', v)}
          />
          Enabled
        </label>
        {!createMode ? (
          <span
            className={
              state === 'registered'
                ? 'text-emerald-600'
                : state === 'failed'
                  ? 'text-destructive'
                  : 'text-muted-foreground'
            }
          >
            {gatewayStatusLabel(state)}
          </span>
        ) : null}
        {snap?.status.error ? <span className="text-destructive">{snap.status.error}</span> : null}
      </div>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div>
          <Label htmlFor="gw-name" className="text-xs">
            Profile name
          </Label>
          <Input
            id="gw-name"
            className="mt-1"
            value={value.name ?? ''}
            onChange={(e) => setField('name', e.target.value)}
          />
        </div>
        <div>
          <Label htmlFor="gw-id" className="text-xs">
            ID
          </Label>
          <Input
            id="gw-id"
            className="mt-1"
            disabled={!createMode}
            placeholder="auto"
            value={value.id ?? ''}
            onChange={(e) => setField('id', e.target.value)}
          />
        </div>
        <div>
          <Label htmlFor="gw-domain" className="text-xs">
            Domain
          </Label>
          <Input
            id="gw-domain"
            className="mt-1"
            value={value.domain}
            onChange={(e) => setField('domain', e.target.value)}
          />
        </div>
        <div>
          <Label htmlFor="gw-addr" className="text-xs">
            Registrar addr
          </Label>
          <Input
            id="gw-addr"
            className="mt-1"
            placeholder="192.168.1.10:5060"
            value={value.addr}
            onChange={(e) => setField('addr', e.target.value)}
          />
        </div>
        <div>
          <Label htmlFor="gw-user" className="text-xs">
            Username
          </Label>
          <Input
            id="gw-user"
            className="mt-1"
            value={value.username}
            onChange={(e) => setField('username', e.target.value)}
          />
        </div>
        <div>
          <Label htmlFor="gw-pass" className="text-xs">
            Password
          </Label>
          <Input
            id="gw-pass"
            type="password"
            className="mt-1"
            placeholder={snap?.config.password === '***' ? 'unchanged' : ''}
            value={value.password ?? ''}
            onChange={(e) => setField('password', e.target.value)}
          />
        </div>
        <div>
          <Label htmlFor="gw-aor" className="text-xs">
            Register user (optional)
          </Label>
          <Input
            id="gw-aor"
            className="mt-1"
            value={value.register_user ?? ''}
            onChange={(e) => setField('register_user', e.target.value)}
          />
        </div>
        <AdvertisedIPField
          value={value.advertised_ip ?? ''}
          onChange={(ip) => setField('advertised_ip', ip)}
          busy={busy}
          bearer={bearer}
        />
        <div>
          <Label htmlFor="gw-exp" className="text-xs">
            Expires (s)
          </Label>
          <Input
            id="gw-exp"
            type="number"
            className="mt-1"
            value={value.register_expires ?? 300}
            onChange={(e) => setField('register_expires', Number(e.target.value) || 300)}
          />
        </div>
        <div>
          <Label htmlFor="gw-ka" className="text-xs">
            Keep-alive (s)
          </Label>
          <Input
            id="gw-ka"
            type="number"
            className="mt-1"
            value={value.keepalive_seconds ?? 20}
            onChange={(e) => setField('keepalive_seconds', Number(e.target.value) || 20)}
          />
        </div>
        <div>
          <Label htmlFor="gw-cport" className="text-xs">
            Contact port (UAS listen)
          </Label>
          <Input
            id="gw-cport"
            type="number"
            className="mt-1"
            value={value.contact_port ?? 5060}
            disabled
            readOnly
          />
          <p className="text-muted-foreground mt-1 text-[11px]">Pinned to the server UDP listener. Change the listener to move Contact.</p>
        </div>
      </div>

      <label className="flex items-center gap-2 text-xs">
        <input
          type="checkbox"
          checked={value.register}
          onChange={(e) => setField('register', e.target.checked)}
        />
        REGISTER to PBX
      </label>

      <p className="text-muted-foreground text-[11px]">
        Contact: <code>{gatewayContactPreview(value)}</code>
      </p>

      {!createMode && value.id ? (
        <>
          <div className="flex flex-col gap-2">
            <p className="text-xs font-medium">Inbound arm</p>
            <p className="text-muted-foreground text-[11px]">
              INVITEs whose Request-URI user matches this AOR run this UAS. Save keeps it across
              restart. Arm applies it immediately without waiting for Save.
            </p>
            <ScenarioSelectField
              label="Armed UAS scenario"
              id="gw-arm"
              value={armId}
              onChange={onArmId}
              scenarios={scenarios}
              builtins={builtins}
              roleFilter="uas"
              allowEmpty={false}
            />
            <p className="text-muted-foreground text-[11px]">
              Armed: <code>{snap?.armed_scenario_id || 'uas'}</code>
            </p>
            <Button type="button" size="sm" onClick={onArm} disabled={busy} className="w-fit">
              Arm
            </Button>
          </div>

          <div className="flex flex-col gap-2">
            <p className="text-xs font-medium">Originate</p>
            <p className="text-muted-foreground text-[11px]">
              Starts a UAC lab job toward the PBX as this AOR.
            </p>
            <ScenarioSelectField
              label="UAC scenario"
              id="gw-orig"
              value={origId}
              onChange={onOrigId}
              scenarios={scenarios}
              builtins={builtins}
              roleFilter="uac"
              allowEmpty={false}
            />
            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div>
                <Label htmlFor="gw-to" className="text-xs">
                  Destination
                </Label>
                <Input
                  id="gw-to"
                  className="mt-1"
                  placeholder="100"
                  value={origTo}
                  onChange={(e) => onOrigTo(e.target.value)}
                />
              </div>
              <div>
                <Label htmlFor="gw-n" className="text-xs">
                  Calls
                </Label>
                <Input
                  id="gw-n"
                  type="number"
                  className="mt-1"
                  value={origCalls}
                  onChange={(e) => onOrigCalls(Math.max(1, Number(e.target.value) || 1))}
                />
              </div>
            </div>
            <Button
              type="button"
              size="sm"
              onClick={onOriginate}
              disabled={busy || !canOrig || !origTo.trim()}
              className="w-fit"
            >
              Originate
            </Button>
          </div>
        </>
      ) : null}
    </div>
  )
}

function AdvertisedIPField({
  value,
  onChange,
  busy,
  bearer,
}: {
  value: string
  onChange: (ip: string) => void
  busy: boolean
  bearer?: string
}) {
  const [discOpen, setDiscOpen] = useState(false)
  const [discBusy, setDiscBusy] = useState(false)
  const [disc, setDisc] = useState<GatewayIPHints | null>(null)
  const [discErr, setDiscErr] = useState<string | null>(null)
  const discRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!discOpen) return
    const onDoc = (ev: MouseEvent) => {
      if (!discRef.current?.contains(ev.target as Node)) {
        setDiscOpen(false)
      }
    }
    document.addEventListener('mousedown', onDoc)
    return () => document.removeEventListener('mousedown', onDoc)
  }, [discOpen])

  const onDiscover = () => {
    if (discOpen) {
      setDiscOpen(false)
      return
    }
    setDiscOpen(true)
    setDiscBusy(true)
    setDiscErr(null)
    void getGatewayIPs({ bearer })
      .then((hints) => setDisc(hints))
      .catch(() => {
        setDisc(null)
        setDiscErr('Could not discover host IPs')
      })
      .finally(() => setDiscBusy(false))
  }

  return (
    <div>
      <Label htmlFor="gw-adv" className="text-xs">
        Advertised IP
      </Label>
      <div className="mt-1 flex gap-1">
        <Input
          id="gw-adv"
          className="min-w-0 flex-1"
          value={value}
          onChange={(e) => onChange(e.target.value)}
        />
        <div className="relative shrink-0" ref={discRef}>
          <Button
            type="button"
            size="sm"
            variant="outline"
            className="h-8"
            aria-haspopup="menu"
            aria-expanded={discOpen}
            disabled={busy || discBusy}
            onClick={onDiscover}
          >
            Autodiscover
          </Button>
          {discOpen ? (
            <div
              role="menu"
              className="border-border bg-popover text-popover-foreground absolute top-full right-0 z-50 mt-1 max-h-72 min-w-64 overflow-y-auto rounded-md border py-1 shadow-md"
            >
              {discBusy ? (
                <p className="text-muted-foreground px-2.5 py-1.5 text-xs">Looking up IPs…</p>
              ) : null}
              {discErr ? <p className="text-destructive px-2.5 py-1.5 text-xs">{discErr}</p> : null}
              {!discBusy && disc ? (
                <>
                  {disc.external ? (
                    <button
                      type="button"
                      role="menuitem"
                      className="hover:bg-muted block w-full px-2.5 py-1.5 text-left text-xs font-medium"
                      onClick={() => {
                        onChange(disc.external as string)
                        setDiscOpen(false)
                      }}
                    >
                      My external IP · {disc.external}
                    </button>
                  ) : (
                    <p className="text-muted-foreground px-2.5 py-1.5 text-xs">
                      My external IP unavailable
                      {disc.external_error ? `: ${disc.external_error}` : ''}
                    </p>
                  )}
                  <div className="border-border my-1 border-t" />
                  {disc.local.length > 0 ? (
                    <>
                      <p className="text-muted-foreground px-2.5 pt-1 pb-0.5 text-[10px] tracking-wide uppercase">
                        Local
                      </p>
                      {disc.local.map((addr) => (
                        <button
                          key={`${addr.iface ?? ''}:${addr.ip}`}
                          type="button"
                          role="menuitem"
                          className="hover:bg-muted block w-full px-2.5 py-1.5 text-left text-xs"
                          onClick={() => {
                            onChange(addr.ip)
                            setDiscOpen(false)
                          }}
                        >
                          {gatewayLocalIPLabel(addr)}
                        </button>
                      ))}
                    </>
                  ) : (
                    <p className="text-muted-foreground px-2.5 py-1.5 text-xs">No local IPv4 found</p>
                  )}
                </>
              ) : null}
            </div>
          ) : null}
        </div>
      </div>
    </div>
  )
}
