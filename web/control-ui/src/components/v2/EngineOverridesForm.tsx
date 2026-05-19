import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'

export type EngineOverrides = {
  total_calls?: number
  rate?: number
  max_concurrent?: number
  sip_from?: string
  sip_pai?: string
  sip_provider?: string
  remote_host?: string
  remote_port?: number
  global_timeout_ms?: number
  health_min_success_ratio?: number
  health_max_failed_calls?: number
}

export type EngineOverridesFormProps = {
  value: EngineOverrides
  onChange: (v: EngineOverrides) => void
  enabled: boolean
  onEnabledChange: (v: boolean) => void
}

export function EngineOverridesForm({ value, onChange, enabled, onEnabledChange }: EngineOverridesFormProps) {
  const set = <K extends keyof EngineOverrides>(k: K, v: EngineOverrides[K]) => onChange({ ...value, [k]: v })

  return (
    <div className="flex flex-col gap-2">
      <label className="flex items-center gap-2 text-xs font-medium">
        <input type="checkbox" checked={enabled} onChange={(e) => onEnabledChange(e.target.checked)} />
        Engine overrides (POST /jobs engine JSON)
      </label>
      {enabled ? (
        <div className="ml-5 grid grid-cols-1 gap-2 md:grid-cols-2">
          <Field label="total_calls" type="number" value={value.total_calls} onNum={(n) => set('total_calls', n)} />
          <Field label="rate" type="number" step="0.1" value={value.rate} onNum={(n) => set('rate', n)} />
          <Field label="max_concurrent" type="number" value={value.max_concurrent} onNum={(n) => set('max_concurrent', n)} />
          <Field label="remote_host" value={value.remote_host} onStr={(s) => set('remote_host', s)} />
          <Field label="remote_port" type="number" value={value.remote_port} onNum={(n) => set('remote_port', n)} />
          <Field label="sip_from" value={value.sip_from} onStr={(s) => set('sip_from', s)} mono />
          <Field label="sip_pai" value={value.sip_pai} onStr={(s) => set('sip_pai', s)} mono />
          <Field label="sip_provider" value={value.sip_provider} onStr={(s) => set('sip_provider', s)} mono />
        </div>
      ) : null}
    </div>
  )
}

function Field({
  label,
  value,
  onNum,
  onStr,
  type = 'text',
  step,
  mono,
}: {
  label: string
  value?: number | string
  onNum?: (n: number | undefined) => void
  onStr?: (s: string) => void
  type?: string
  step?: string
  mono?: boolean
}) {
  return (
    <div className="flex flex-col gap-0.5">
      <Label className="text-[10px]">{label}</Label>
      <Input
        type={type}
        step={step}
        className={`h-8 text-xs ${mono ? 'font-mono' : ''}`}
        value={value ?? ''}
        onChange={(e) => {
          if (onNum) {
            const n = e.target.value === '' ? undefined : Number(e.target.value)
            onNum(Number.isFinite(n!) ? n : undefined)
          } else onStr?.(e.target.value)
        }}
      />
    </div>
  )
}

export function buildEnginePayload(v: EngineOverrides): Record<string, unknown> | undefined {
  const out: Record<string, unknown> = {}
  for (const [k, val] of Object.entries(v)) {
    if (val !== undefined && val !== '' && !(typeof val === 'number' && Number.isNaN(val))) out[k] = val
  }
  return Object.keys(out).length ? out : undefined
}
