import type { ScenarioRoleFilter } from '@/lib/scenarioSelect'
import { buildScenarioOptions } from '@/lib/scenarioSelect'
import type { BuiltinScenarioMeta, ScenarioMeta } from '@/api/v2'
import { Label } from '@/components/ui/label'

export type ScenarioSelectProps = {
  value: string
  onChange: (id: string) => void
  scenarios: ScenarioMeta[]
  builtins: BuiltinScenarioMeta[]
  roleFilter?: ScenarioRoleFilter
  allowEmpty?: boolean
  emptyLabel?: string
  className?: string
  id?: string
}

export function ScenarioSelect({
  value,
  onChange,
  scenarios,
  builtins,
  roleFilter = 'any',
  allowEmpty = true,
  emptyLabel = '(none)',
  className,
  id,
}: ScenarioSelectProps) {
  const options = buildScenarioOptions(scenarios, builtins, roleFilter)
  const custom = options.filter((o) => o.group === 'custom')
  const built = options.filter((o) => o.group === 'builtin')

  return (
    <select
      id={id}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      className={
        className ??
        'border-input bg-background mt-1 w-full rounded-md border px-2 py-1.5 text-sm'
      }
    >
      {allowEmpty ? <option value="">{emptyLabel}</option> : null}
      {custom.length > 0 ? (
        <optgroup label="Custom scenarios">
          {custom.map((o) => (
            <option key={o.id} value={o.id}>
              {o.label}
            </option>
          ))}
        </optgroup>
      ) : null}
      {built.length > 0 ? (
        <optgroup label="Built-in (read-only)">
          {built.map((o) => (
            <option key={o.id} value={o.id}>
              {o.label}
            </option>
          ))}
        </optgroup>
      ) : null}
    </select>
  )
}

export type ScenarioPreviewProps = {
  scenarioId: string
  scenarios: ScenarioMeta[]
  builtins: BuiltinScenarioMeta[]
}

export function ScenarioPreview({ scenarioId, scenarios, builtins }: ScenarioPreviewProps) {
  if (!scenarioId) return null
  const sc =
    scenarios.find((x) => x.id === scenarioId) ?? builtins.find((x) => x.id === scenarioId)
  if (!sc) return null
  const builtin = 'source' in sc && sc.source === 'builtin'
  return (
    <div className="text-muted-foreground mt-1 rounded-md border px-2 py-1 text-[11px]">
      <div>
        <span className="text-foreground/70">role:</span> <code>{sc.role ?? 'either'}</code>
        {builtin ? (
          <>
            {' '}
            · <span className="text-warning">built-in</span>
          </>
        ) : null}
      </div>
      {sc.description ? (
        <div className="mt-0.5">
          <span className="text-foreground/70">desc:</span> {sc.description}
        </div>
      ) : null}
    </div>
  )
}

export function ScenarioSelectField(props: ScenarioSelectProps & { label: string }) {
  const { label, ...rest } = props
  return (
    <div>
      <Label className="text-xs">{label}</Label>
      <ScenarioSelect {...rest} />
    </div>
  )
}
