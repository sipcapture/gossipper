import { useCallback, useEffect, useMemo, useState } from 'react'

import {
  getControl,
  getHealth,
  getScenario,
  getStats,
  postControl,
  postScenarioApply,
  putScenario,
  type ApiErrorShape,
  type ControlState,
  type ScenarioGetResponse,
  type StatsSummary,
} from '@/api/gossipper'
import { PRESET_OPTIONS_CLIENT, PRESET_OPTIONS_SERVER } from '@/api/presets'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'

const LS_TOKEN = 'gossipper_control_api_token'

function readStoredToken(): string {
  try {
    return localStorage.getItem(LS_TOKEN) ?? ''
  } catch {
    return ''
  }
}

function isApiError(e: unknown): e is ApiErrorShape {
  return (
    typeof e === 'object' &&
    e !== null &&
    'status' in e &&
    'message' in e &&
    typeof (e as ApiErrorShape).message === 'string'
  )
}

function errText(e: unknown): string {
  if (isApiError(e)) return `${e.status}: ${e.message}`
  if (e instanceof Error) return e.message
  return String(e)
}

export default function App() {
  const [bearerDraft, setBearerDraft] = useState(readStoredToken)
  const [bearer, setBearer] = useState<string | undefined>(() => {
    const t = readStoredToken().trim()
    return t === '' ? undefined : t
  })

  const [health, setHealth] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [lastError, setLastError] = useState<string | null>(null)

  const [scenarioMeta, setScenarioMeta] = useState<ScenarioGetResponse | null>(null)
  const [scenarioXml, setScenarioXml] = useState('')

  const [control, setControl] = useState<ControlState | null>(null)
  const [rateDraft, setRateDraft] = useState('')

  const [stats, setStats] = useState<StatsSummary | null>(null)
  const [pollStats, setPollStats] = useState(true)

  const saveToken = useCallback(() => {
    const t = bearerDraft.trim()
    if (t) {
      localStorage.setItem(LS_TOKEN, t)
      setBearer(t)
    } else {
      localStorage.removeItem(LS_TOKEN)
      setBearer(undefined)
    }
  }, [bearerDraft])

  const run = useCallback(
    async <T,>(fn: () => Promise<T>): Promise<T | undefined> => {
      setBusy(true)
      setLastError(null)
      try {
        return await fn()
      } catch (e) {
        setLastError(errText(e))
        return undefined
      } finally {
        setBusy(false)
      }
    },
    [],
  )

  const refreshHealth = useCallback(() => {
    void run(async () => {
      const h = await getHealth(bearer)
      setHealth(h.status)
    })
  }, [bearer, run])

  const refreshControl = useCallback(() => {
    void run(async () => {
      const c = await getControl(bearer)
      setControl(c)
      setRateDraft(String(c.rate))
    })
  }, [bearer, run])

  const loadScenario = useCallback(() => {
    void run(async () => {
      const s = await getScenario(bearer)
      setScenarioMeta(s)
      setScenarioXml(s.xml ?? '')
    })
  }, [bearer, run])

  const refreshStats = useCallback(() => {
    void run(async () => {
      const s = await getStats(bearer)
      setStats(s)
    })
  }, [bearer, run])

  useEffect(() => {
    const t = window.setTimeout(() => {
      void refreshHealth()
      void refreshControl()
      void loadScenario()
    }, 0)
    return () => window.clearTimeout(t)
  }, [refreshHealth, refreshControl, loadScenario])

  useEffect(() => {
    if (!pollStats) return
    const id = window.setInterval(() => {
      void refreshStats()
    }, 2000)
    return () => window.clearInterval(id)
  }, [pollStats, refreshStats])

  const statsJson = useMemo(
    () => (stats ? JSON.stringify(stats, null, 2) : ''),
    [stats],
  )

  const builtin = scenarioMeta?.builtin === true

  return (
    <div className="bg-background text-foreground min-h-screen">
      <div className="mx-auto flex max-w-5xl flex-col gap-4 p-4">
        <header className="flex flex-col gap-1 border-b pb-4">
          <h1 className="font-heading text-lg font-medium">Gossipper — Control</h1>
          <p className="text-muted-foreground max-w-2xl text-xs/relaxed">
            Веб-панель для HTTP API управления (
            <code className="text-foreground/80">/api/v1</code>
            ). В dev режиме Vite проксирует{' '}
            <code className="text-foreground/80">/api</code> на{' '}
            <code className="text-foreground/80">VITE_API_TARGET</code>.
          </p>
        </header>

        {lastError ? (
          <div
            className="border-destructive/50 bg-destructive/10 text-destructive px-3 py-2 text-xs ring-1 ring-destructive/20"
            role="alert"
          >
            {lastError}
          </div>
        ) : null}

        <Card>
          <CardHeader>
            <CardTitle>Подключение</CardTitle>
            <CardDescription>
              Если gossipper запущен с <code>-api_token</code>, введите тот же секрет
              (Bearer). Хранится в <code>localStorage</code>.
            </CardDescription>
          </CardHeader>
          <CardContent className="flex flex-col gap-3 sm:flex-row sm:items-end">
            <div className="flex min-w-0 flex-1 flex-col gap-1.5">
              <Label htmlFor="token">API token (опционально)</Label>
              <Input
                id="token"
                type="password"
                autoComplete="off"
                placeholder="Bearer secret"
                value={bearerDraft}
                onChange={(e) => setBearerDraft(e.target.value)}
              />
            </div>
            <Button type="button" variant="secondary" disabled={busy} onClick={saveToken}>
              Сохранить токен
            </Button>
          </CardContent>
          <CardFooter className="flex flex-wrap gap-2">
            <Button type="button" variant="outline" size="sm" disabled={busy} onClick={refreshHealth}>
              Health
            </Button>
            <span className="text-muted-foreground self-center text-xs">
              статус:{' '}
              <span className={cn(health === 'ok' ? 'text-success' : 'text-foreground')}>
                {health ?? '—'}
              </span>
            </span>
          </CardFooter>
        </Card>

        <div className="grid gap-4 lg:grid-cols-2">
          <Card className="min-h-[320px]">
            <CardHeader>
              <CardTitle>Сценарий XML</CardTitle>
              <CardDescription>
                <code>GET/PUT /api/v1/scenario</code> (нужен <code>-sf</code> для записи),{' '}
                <code>POST /api/v1/scenario/apply</code> — горячая подмена в движке.
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-2">
              {scenarioMeta ? (
                <p className="text-muted-foreground text-xs">
                  файл: <code>{scenarioMeta.scenario_file || '—'}</code>, имя:{' '}
                  <code>{scenarioMeta.scenario_name || '—'}</code>
                  {builtin ? (
                    <span className="text-warning ml-2">(встроенный — PUT недоступен)</span>
                  ) : null}
                </p>
              ) : null}
              <Tabs defaultValue="editor">
                <TabsList variant="line" className="w-full max-w-md">
                  <TabsTrigger value="editor">Редактор</TabsTrigger>
                  <TabsTrigger value="preset-uac">Пресет UAC</TabsTrigger>
                  <TabsTrigger value="preset-uas">Пресет UAS</TabsTrigger>
                </TabsList>
                <TabsContent value="editor" className="mt-2">
                  <Textarea
                    className="font-mono min-h-[220px] text-[11px] leading-snug"
                    spellCheck={false}
                    value={scenarioXml}
                    onChange={(e) => setScenarioXml(e.target.value)}
                  />
                </TabsContent>
                <TabsContent value="preset-uac" className="mt-2">
                  <pre className="border-input bg-muted/30 max-h-48 overflow-auto border p-2 font-mono text-[11px] leading-snug whitespace-pre-wrap">
                    {PRESET_OPTIONS_CLIENT}
                  </pre>
                  <Button
                    type="button"
                    className="mt-2"
                    size="sm"
                    variant="secondary"
                    onClick={() => setScenarioXml(PRESET_OPTIONS_CLIENT)}
                  >
                    Вставить в редактор
                  </Button>
                </TabsContent>
                <TabsContent value="preset-uas" className="mt-2">
                  <pre className="border-input bg-muted/30 max-h-48 overflow-auto border p-2 font-mono text-[11px] leading-snug whitespace-pre-wrap">
                    {PRESET_OPTIONS_SERVER}
                  </pre>
                  <Button
                    type="button"
                    className="mt-2"
                    size="sm"
                    variant="secondary"
                    onClick={() => setScenarioXml(PRESET_OPTIONS_SERVER)}
                  >
                    Вставить в редактор
                  </Button>
                </TabsContent>
              </Tabs>
            </CardContent>
            <CardFooter className="flex flex-wrap gap-2">
              <Button type="button" size="sm" variant="outline" disabled={busy} onClick={loadScenario}>
                Загрузить с сервера
              </Button>
              <Button
                type="button"
                size="sm"
                variant="outline"
                disabled={busy || builtin}
                onClick={() =>
                  run(async () => {
                    await putScenario(scenarioXml, { apply: false, bearer })
                    await loadScenario()
                  })
                }
              >
                Сохранить в файл
              </Button>
              <Button
                type="button"
                size="sm"
                variant="secondary"
                disabled={busy || builtin}
                onClick={() =>
                  run(async () => {
                    await putScenario(scenarioXml, { apply: true, bearer })
                    await loadScenario()
                    await refreshControl()
                  })
                }
              >
                Сохранить + apply
              </Button>
              <Button
                type="button"
                size="sm"
                disabled={busy}
                onClick={() =>
                  run(async () => {
                    await postScenarioApply(scenarioXml, bearer)
                    await refreshControl()
                  })
                }
              >
                Apply (тело или файл)
              </Button>
            </CardFooter>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Управление / stats</CardTitle>
              <CardDescription>
                <code>GET/POST /api/v1/control</code> — rate и pause.{' '}
                <code>GET /api/v1/stats</code> — снимок счётчиков.
              </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              {control ? (
                <div className="flex flex-col gap-3">
                  <div className="flex flex-wrap items-center gap-3">
                    <Label htmlFor="paused" className="shrink-0">
                      Пауза
                    </Label>
                    <Switch
                      id="paused"
                      checked={control.paused}
                      onCheckedChange={(v) => {
                        void run(async () => {
                          const next = await postControl({ paused: v }, bearer)
                          setControl(next)
                          setRateDraft(String(next.rate))
                        })
                      }}
                    />
                  </div>
                  <div className="flex flex-wrap items-end gap-2">
                    <div className="flex min-w-[120px] flex-col gap-1.5">
                      <Label htmlFor="rate">Rate (calls/s)</Label>
                      <Input
                        id="rate"
                        type="number"
                        step="any"
                        value={rateDraft}
                        onChange={(e) => setRateDraft(e.target.value)}
                      />
                    </div>
                    <Button
                      type="button"
                      size="sm"
                      variant="secondary"
                      disabled={busy}
                      onClick={() => {
                        const n = Number(rateDraft)
                        if (Number.isNaN(n)) return
                        void run(async () => {
                          const next = await postControl({ rate: n }, bearer)
                          setControl(next)
                          setRateDraft(String(next.rate))
                        })
                      }}
                    >
                      Применить rate
                    </Button>
                    <Button type="button" size="sm" variant="ghost" disabled={busy} onClick={refreshControl}>
                      Обновить
                    </Button>
                  </div>
                </div>
              ) : (
                <p className="text-muted-foreground text-xs">control: нет данных</p>
              )}

              <div className="flex items-center gap-2">
                <Switch id="poll" checked={pollStats} onCheckedChange={setPollStats} />
                <Label htmlFor="poll">Опрос stats каждые 2 с</Label>
                <Button type="button" size="sm" variant="outline" disabled={busy} onClick={refreshStats}>
                  Stats сейчас
                </Button>
              </div>
              <pre className="border-input bg-muted/20 max-h-56 overflow-auto border p-2 font-mono text-[10px] leading-snug">
                {statsJson || '—'}
              </pre>
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  )
}
