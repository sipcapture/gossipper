import { useCallback, useEffect, useState } from 'react'

import { getHealthV2, rotateJwtSecret, type HealthV2 } from '@/api/v2'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { ThemeToggle, type ThemeMode } from '@/components/ThemeToggle'

export type SettingsV2Props = {
  bearer?: string
  theme: ThemeMode
  onThemeChange: (m: ThemeMode) => void
  onSignOut?: () => void
  authKind: 'none' | 'internal' | null
}

export function SettingsV2({ bearer, theme, onThemeChange, onSignOut, authKind }: SettingsV2Props) {
  const [health, setHealth] = useState<HealthV2 | null>(null)
  const [rotated, setRotated] = useState<{ secret: string; warning: string } | null>(null)
  const [rotating, setRotating] = useState(false)
  const [rotateError, setRotateError] = useState<string | null>(null)
  const refresh = useCallback(async () => {
    try {
      setHealth(await getHealthV2({ bearer }))
    } catch {
      setHealth(null)
    }
  }, [bearer])
  useEffect(() => {
    void refresh()
  }, [refresh])

  const onRotate = async () => {
    if (!window.confirm(
      'Rotate JWT signing secret? Every signed-in session (yours included) will be ' +
        'invalidated and have to sign in again.',
    )) return
    setRotating(true)
    setRotateError(null)
    try {
      const res = await rotateJwtSecret({ bearer })
      setRotated({ secret: res.jwt_secret, warning: res.warning })
    } catch (err) {
      setRotateError(err instanceof Error ? err.message : String(err))
    } finally {
      setRotating(false)
    }
  }

  return (
    <section className="flex flex-col gap-3">
      <Card>
        <CardHeader>
          <CardTitle>System</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="grid grid-cols-2 gap-x-3 gap-y-1 text-xs">
            <dt className="text-muted-foreground">API status</dt>
            <dd>{health?.status ?? '—'}</dd>
            <dt className="text-muted-foreground">Auth mode</dt>
            <dd>{health?.auth ?? authKind ?? '—'}</dd>
            <dt className="text-muted-foreground">Version</dt>
            <dd>{health?.version ?? '—'}</dd>
          </dl>
          <div className="mt-3 flex gap-2">
            <Button type="button" size="sm" variant="outline" onClick={() => void refresh()}>
              Refresh
            </Button>
            {authKind === 'internal' && onSignOut ? (
              <Button type="button" size="sm" variant="destructive" onClick={onSignOut}>
                Sign out
              </Button>
            ) : null}
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Appearance</CardTitle>
        </CardHeader>
        <CardContent>
          <ThemeToggle value={theme} onChange={onThemeChange} />
        </CardContent>
      </Card>

      {authKind === 'internal' ? (
        <Card>
          <CardHeader>
            <CardTitle>JWT signing secret</CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-muted-foreground text-xs">
              Rotate the HMAC key used to sign session tokens. All currently signed-in users
              (including this session) will be forced to re-authenticate.
            </p>
            <div className="mt-2">
              <Button type="button" size="sm" variant="destructive" disabled={rotating} onClick={() => void onRotate()}>
                {rotating ? 'Rotating…' : 'Rotate now'}
              </Button>
            </div>
            {rotateError ? (
              <p className="text-destructive mt-2 text-xs">{rotateError}</p>
            ) : null}
            {rotated ? (
              <div className="border-border bg-muted/40 mt-3 rounded-md border p-2 text-xs">
                <div className="text-foreground font-medium">New secret (save it now):</div>
                <pre className="mt-1 font-mono text-[11px] break-all whitespace-pre-wrap">
                  {rotated.secret}
                </pre>
                <p className="text-muted-foreground mt-1">{rotated.warning}</p>
                {onSignOut ? (
                  <Button type="button" size="xs" className="mt-2" onClick={onSignOut}>
                    Sign out now
                  </Button>
                ) : null}
              </div>
            ) : null}
          </CardContent>
        </Card>
      ) : null}

      <Card>
        <CardHeader>
          <CardTitle>Phase roadmap</CardTitle>
        </CardHeader>
        <CardContent>
          <ul className="text-muted-foreground space-y-1 text-xs">
            <li>
              <strong className="text-foreground">Phase 0 ✓</strong> — supervisor, uistore, SQLite migrations, <code>/api/v2</code>.
            </li>
            <li>
              <strong className="text-foreground">Phase 1 ✓</strong> — admin console UI for profiles / scenarios / jobs / media.
            </li>
            <li>
              <strong className="text-foreground">Phase 2 ✓</strong> — real fork/exec worker runner.
            </li>
            <li>
              <strong className="text-foreground">Phase 3 ✓</strong> — recording artifacts surfaced in jobs detail.
            </li>
            <li>
              <strong className="text-foreground">Phase 4 ✓</strong> — WS/WSS module and engine
              wiring (<code>w1/wn/ws1/wsn</code>).
            </li>
            <li>
              <strong className="text-foreground">Phase 4.2 ◐</strong> — WebRTC bridge
              (<code>internal/webrtc</code>, pion/webrtc/v4) is ready and unit-tested;
              automatic engine binding per call is the next step. See
              <code>docs/webrtc.md</code>.
            </li>
            <li>
              <strong className="text-foreground">Phase 5 ✓</strong> — users CRUD, audit log, docs/ui-mode.md.
            </li>
            <li>
              <strong className="text-foreground">Phase 5.1 ✓</strong> — live WS, JSONL events stream,
              server/client start-stop shortcuts, WAV/PCAP validation, JWT rotation.
            </li>
          </ul>
        </CardContent>
      </Card>
    </section>
  )
}
