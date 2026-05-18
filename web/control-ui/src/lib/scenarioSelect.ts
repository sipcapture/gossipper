import { useMemo } from 'react'

import type { BuiltinScenarioMeta, ScenarioMeta } from '@/api/v2'

export type ScenarioRoleFilter = 'uac' | 'uas' | 'both' | 'any'

export function scenarioMatchesRole(role: string | undefined, filter: ScenarioRoleFilter): boolean {
  if (filter === 'any') return true
  const r = (role ?? '').trim().toLowerCase()
  if (r === '' || r === 'both' || r === 'either') return true
  if (filter === 'both') return r === 'both' || r === 'either'
  return r === filter || r === 'both' || r === 'either'
}

export function filterScenariosByRole(
  scenarios: ScenarioMeta[],
  filter: ScenarioRoleFilter,
): ScenarioMeta[] {
  return scenarios.filter((s) => scenarioMatchesRole(s.role, filter))
}

export function filterBuiltinsByRole(
  builtins: BuiltinScenarioMeta[],
  filter: ScenarioRoleFilter,
): BuiltinScenarioMeta[] {
  return builtins.filter((s) => scenarioMatchesRole(s.role, filter))
}

export type ScenarioSelectOption = {
  id: string
  label: string
  group: 'custom' | 'builtin'
  role?: string
}

export function buildScenarioOptions(
  scenarios: ScenarioMeta[],
  builtins: BuiltinScenarioMeta[],
  roleFilter: ScenarioRoleFilter = 'any',
): ScenarioSelectOption[] {
  const custom = filterScenariosByRole(scenarios, roleFilter).map((s) => ({
    id: s.id,
    label: `${s.id} — ${s.name}${s.role ? ` [${s.role}]` : ''}`,
    group: 'custom' as const,
    role: s.role,
  }))
  const built = filterBuiltinsByRole(builtins, roleFilter).map((s) => ({
    id: s.id,
    label: `${s.id} — ${s.name} [built-in]`,
    group: 'builtin' as const,
    role: s.role,
  }))
  return [...custom, ...built]
}

export function findScenarioMeta(
  id: string,
  scenarios: ScenarioMeta[],
  builtins: BuiltinScenarioMeta[],
): ScenarioMeta | BuiltinScenarioMeta | undefined {
  return scenarios.find((s) => s.id === id) ?? builtins.find((s) => s.id === id)
}

export function useScenarioOptions(
  scenarios: ScenarioMeta[],
  builtins: BuiltinScenarioMeta[],
  roleFilter: ScenarioRoleFilter,
) {
  return useMemo(
    () => buildScenarioOptions(scenarios, builtins, roleFilter),
    [scenarios, builtins, roleFilter],
  )
}
