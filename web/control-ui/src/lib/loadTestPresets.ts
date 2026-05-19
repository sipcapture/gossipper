import type { LoadTestDraft } from '@/lib/loadTestWizard'
import { defaultLoadTestDraft } from '@/lib/loadTestWizard'

const LS_KEY = 'gossipper_load_test_presets'

export type LoadTestPreset = { name: string; draft: LoadTestDraft; saved_at: string }

export function listLoadTestPresets(): LoadTestPreset[] {
  try {
    const raw = localStorage.getItem(LS_KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw) as LoadTestPreset[]
    return Array.isArray(parsed) ? parsed : []
  } catch {
    return []
  }
}

export function saveLoadTestPreset(name: string, draft: LoadTestDraft): LoadTestPreset[] {
  const trimmed = name.trim()
  if (!trimmed) return listLoadTestPresets()
  const next: LoadTestPreset = { name: trimmed, draft: { ...draft, job_id: '' }, saved_at: new Date().toISOString() }
  const all = listLoadTestPresets().filter((p) => p.name !== trimmed)
  all.unshift(next)
  localStorage.setItem(LS_KEY, JSON.stringify(all.slice(0, 20)))
  return all
}

export function deleteLoadTestPreset(name: string): LoadTestPreset[] {
  const all = listLoadTestPresets().filter((p) => p.name !== name)
  localStorage.setItem(LS_KEY, JSON.stringify(all))
  return all
}

export function applyPresetDefaults(): LoadTestDraft {
  return defaultLoadTestDraft()
}
