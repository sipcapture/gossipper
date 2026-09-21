export const PENDING_NEW_SCENARIO_KEY = 'gossipper.scenario.pending_new'

export type PendingScenarioDraft = {
  id: string
  name: string
  description?: string
  role?: string
  xml: string
}

type DraftStore = {
  getItem: (key: string) => string | null
  setItem: (key: string, value: string) => void
  removeItem: (key: string) => void
}

function browserStore(): DraftStore | null {
  try {
    return localStorage
  } catch {
    return null
  }
}

export function stashPendingNewScenario(draft: PendingScenarioDraft, store: DraftStore | null = browserStore()) {
  if (!store) return
  try {
    store.setItem(PENDING_NEW_SCENARIO_KEY, JSON.stringify(draft))
  } catch {
    /* ignore quota / private mode */
  }
}

export function peekPendingNewScenario(store: DraftStore | null = browserStore()): PendingScenarioDraft | null {
  if (!store) return null
  try {
    const raw = store.getItem(PENDING_NEW_SCENARIO_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as PendingScenarioDraft
    if (!parsed || typeof parsed.xml !== 'string') return null
    return {
      id: typeof parsed.id === 'string' ? parsed.id : '',
      name: typeof parsed.name === 'string' ? parsed.name : '',
      description: typeof parsed.description === 'string' ? parsed.description : undefined,
      role: typeof parsed.role === 'string' ? parsed.role : undefined,
      xml: parsed.xml,
    }
  } catch {
    return null
  }
}

export function clearPendingNewScenario(store: DraftStore | null = browserStore()) {
  if (!store) return
  try {
    store.removeItem(PENDING_NEW_SCENARIO_KEY)
  } catch {
    /* ignore */
  }
}

export function takePendingNewScenario(store: DraftStore | null = browserStore()): PendingScenarioDraft | null {
  const parsed = peekPendingNewScenario(store)
  if (parsed) clearPendingNewScenario(store)
  return parsed
}

export function sourceLabel(source: 'store' | 'builtin' | 'lab'): string {
  if (source === 'store') return 'Mine'
  if (source === 'lab') return 'Lab'
  return 'Built-in'
}

export function roleMatchesFilter(
  role: string | undefined,
  filter: 'all' | 'server' | 'client' | 'either',
): boolean {
  if (filter === 'all') return true
  const r = (role ?? '').toLowerCase()
  const isEither = r === '' || r === 'either' || r === 'any'
  if (filter === 'either') return isEither
  if (filter === 'server') return r === 'server' || r === 'uas'
  if (filter === 'client') return r === 'client' || r === 'uac'
  return true
}
