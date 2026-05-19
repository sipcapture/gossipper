import type { Job } from '@/api/v2'

export type JobKindFilter = 'all' | 'load_test' | 'tool' | 'server' | 'client' | 'running'

export const LOAD_WIZARD_PROFILE = '_load_wizard'

export function classifyJob(j: Job): Exclude<JobKindFilter, 'all' | 'running'> {
  if (j.profile_kind === 'tool') return 'tool'
  if (j.profile_kind === 'server') return 'server'
  if (j.profile_id === LOAD_WIZARD_PROFILE) return 'load_test'
  return 'client'
}

export function jobMatchesKindFilter(j: Job, filter: JobKindFilter): boolean {
  if (filter === 'all') return true
  if (filter === 'running') return j.status === 'running' || j.status === 'pending'
  return classifyJob(j) === filter
}
