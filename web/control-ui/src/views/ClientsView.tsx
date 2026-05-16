import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'

type Props = {
  busy: boolean
  clientSnippet: string
  onClientSnippet: (v: string) => void
  clientWantId: string
  onClientWantId: (v: string) => void
  dynamicList: string[]
  dynFromStats: string[]
  onRefreshList: () => void
  onStartClient: () => void
  onRemoveClient: (id: string) => void
}

export function ClientsView({
  busy,
  clientSnippet,
  onClientSnippet,
  clientWantId,
  onClientWantId,
  dynamicList,
  dynFromStats,
  onRefreshList,
  onStartClient,
  onRemoveClient,
}: Props) {
  const ids = dynamicList.length ? dynamicList : dynFromStats

  return (
    <div className="flex max-w-4xl flex-col gap-4">
      <p className="text-muted-foreground text-[11px] leading-relaxed">
        <code className="text-foreground/80">POST /clients</code> starts an extra UAC;{' '}
        <code className="text-foreground/80">DELETE ?id=</code> stops it. Management mode with{' '}
        <code className="text-foreground/80">api_addr</code> only.
      </p>

      <div className="flex flex-wrap gap-2">
        <Button type="button" size="sm" variant="outline" disabled={busy} onClick={onRefreshList}>
          Refresh list
        </Button>
      </div>

      <div className="border-border overflow-hidden border">
        <div className="bg-muted/40 border-border text-muted-foreground border-b px-3 py-1.5 text-[11px] font-medium tracking-wide uppercase">
          Active ids
        </div>
        <ul className="divide-border divide-y">
          {ids.length === 0 ? (
            <li className="text-muted-foreground px-3 py-4 text-sm">No dynamic clients</li>
          ) : (
            ids.map((id) => (
              <li key={id} className="flex items-center justify-between gap-3 px-3 py-2">
                <code className="text-xs">{id}</code>
                <Button type="button" size="sm" variant="destructive" disabled={busy} onClick={() => onRemoveClient(id)}>
                  Stop
                </Button>
              </li>
            ))
          )}
        </ul>
      </div>

      <div className="grid max-w-xl gap-2">
        <Label htmlFor="cid">Desired id (optional)</Label>
        <Input
          id="cid"
          className="font-mono text-xs"
          value={clientWantId}
          onChange={(e) => onClientWantId(e.target.value)}
          placeholder="e.g. extra-1"
        />
      </div>

      <div className="flex flex-col gap-1.5">
        <Label htmlFor="snippet">Client JSON snippet</Label>
        <Textarea
          id="snippet"
          className="font-mono min-h-[220px] text-[11px] leading-snug"
          spellCheck={false}
          value={clientSnippet}
          onChange={(e) => onClientSnippet(e.target.value)}
        />
      </div>

      <Button type="button" disabled={busy} onClick={onStartClient} className="w-fit">
        Start client
      </Button>

      <p className="text-muted-foreground text-[11px]">
        To change a running client&apos;s scenario: stop by id, then create again with the same id.
      </p>
    </div>
  )
}
