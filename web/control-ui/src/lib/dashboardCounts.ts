import type { GatewaySnapshot } from '@/api/v2'

export function tallyScenarioCatalog(mine: number, catalog: number): { all: number; ours: number } {
  const ours = Math.max(0, mine)
  return { ours, all: ours + Math.max(0, catalog) }
}

export function tallyGateways(gws: GatewaySnapshot[]): { total: number; enabled: number } {
  return {
    total: gws.length,
    enabled: gws.filter((g) => g.config?.enabled).length,
  }
}
