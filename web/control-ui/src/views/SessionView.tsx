import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ThemeToggle, type ThemeMode } from '@/components/ThemeToggle'
import { cn } from '@/lib/utils'

type Props = {
  authKind: 'none' | 'internal'
  busy: boolean
  health: string | null
  bearerDraft: string
  onBearerDraft: (v: string) => void
  onSaveToken: () => void
  onRefreshHealth: () => void
  onSignOut: () => void
  theme: ThemeMode
  onThemeChange: (mode: ThemeMode) => void
}

export function SessionView({
  authKind,
  busy,
  health,
  bearerDraft,
  onBearerDraft,
  onSaveToken,
  onRefreshHealth,
  onSignOut,
  theme,
  onThemeChange,
}: Props) {
  if (authKind === 'internal') {
    return (
      <div className="flex max-w-lg flex-col gap-4">
        <Card>
          <CardHeader>
            <CardTitle>API session</CardTitle>
            <CardDescription>
              <code className="text-foreground/90">auth.type: internal</code> is enabled — access uses JWT after
              sign-in.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-wrap items-center gap-3">
            <Button type="button" variant="outline" size="sm" disabled={busy} onClick={onSignOut}>
              Sign out
            </Button>
            <Button type="button" variant="outline" size="sm" disabled={busy} onClick={onRefreshHealth}>
              Health
            </Button>
            <span className="text-muted-foreground text-xs">
              status:{' '}
              <span className={cn(health === 'ok' ? 'text-success' : 'text-foreground')}>{health ?? '—'}</span>
            </span>
          </CardContent>
        </Card>
        <ThemeToggle value={theme} onChange={onThemeChange} />
      </div>
    )
  }

  return (
    <div className="flex max-w-lg flex-col gap-4">
      <Card>
        <CardHeader>
          <CardTitle>API connection</CardTitle>
          <CardDescription>
            With <code className="text-foreground/90">-api_token</code>, the token is sent as Bearer and in the
            WebSocket query <code className="text-foreground/90">?token=</code>.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-3 sm:flex-row sm:items-end">
          <div className="flex min-w-0 flex-1 flex-col gap-1.5">
            <Label htmlFor="token">API token (optional)</Label>
            <Input
              id="token"
              type="password"
              autoComplete="off"
              placeholder="Bearer secret"
              value={bearerDraft}
              onChange={(e) => onBearerDraft(e.target.value)}
            />
          </div>
          <Button type="button" variant="secondary" disabled={busy} onClick={onSaveToken}>
            Save
          </Button>
        </CardContent>
        <CardFooter className="flex flex-wrap gap-2">
          <Button type="button" variant="outline" size="sm" disabled={busy} onClick={onRefreshHealth}>
            Health
          </Button>
          <span className="text-muted-foreground self-center text-xs">
            status:{' '}
            <span className={cn(health === 'ok' ? 'text-success' : 'text-foreground')}>{health ?? '—'}</span>
          </span>
        </CardFooter>
      </Card>
      <ThemeToggle value={theme} onChange={onThemeChange} />
    </div>
  )
}
