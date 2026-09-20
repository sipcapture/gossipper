import { useEffect, useMemo, useRef, useState, type CSSProperties, type DragEvent, type PointerEvent, type ReactNode } from 'react'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import {
  BLOCK_ICONS,
  BLOCK_TYPES,
  GRID,
  addBlock,
  blockMeta,
  blocksToXML,
  canAddBlock,
  cardHint,
  edgeLabel,
  moveBlock,
  removeBlock,
  snapToGrid,
  xmlToBlocks,
  type GraphBlock,
  type GraphBlockType,
} from '@/lib/scenarioGraph'
import { cn } from '@/lib/utils'

const NODE_W = 160
const NODE_H = 80
const NODE_GAP = 48
const NODE_X = 112
const NODE_Y0 = 32
const MIME = 'application/x-gossipper-block'

function BlockIcon({ type, size = 22 }: { type: GraphBlockType; size?: number }) {
  const icon = BLOCK_ICONS[type]
  if (!icon) return null
  return (
    <svg className="block" width={size} height={size} viewBox={icon.viewBox} aria-hidden="true">
      <path d={icon.d} fill="currentColor" />
    </svg>
  )
}

function defaultLayout(n: number) {
  return Array.from({ length: n }, (_, i) => ({
    x: NODE_X,
    y: NODE_Y0 + i * (NODE_H + NODE_GAP),
  }))
}

function edgeGeometry(a: { x: number; y: number }, b: { x: number; y: number }) {
  const x1 = a.x + NODE_W / 2
  const y1 = a.y + NODE_H
  const x2 = b.x + NODE_W / 2
  const y2 = b.y
  const mid = (y1 + y2) / 2
  return {
    d: `M ${x1} ${y1} C ${x1} ${mid}, ${x2} ${mid}, ${x2} ${y2}`,
    mx: (x1 + x2) / 2,
    my: (y1 + y2) / 2,
  }
}

function clampPos(x: number, y: number, snap: boolean) {
  let nx = Math.max(0, x)
  let ny = Math.max(0, y)
  if (snap) {
    nx = Math.max(0, snapToGrid(nx))
    ny = Math.max(0, snapToGrid(ny))
  }
  return { x: nx, y: ny }
}

function BlockFields({ block, onPatch }: { block: GraphBlock; onPatch: (p: Partial<GraphBlock>) => void }) {
  switch (block.type) {
    case 'start':
      return (
        <Field label="scenario name">
          <Input value={block.name || ''} onChange={(e) => onPatch({ name: e.target.value })} />
        </Field>
      )
    case 'send':
      return (
        <>
          <Field label="retrans (ms, optional)">
            <Input value={block.retrans || ''} onChange={(e) => onPatch({ retrans: e.target.value })} />
          </Field>
          <Field label="SIP (CDATA)">
            <Textarea
              value={block.text || ''}
              onChange={(e) => onPatch({ text: e.target.value })}
              className="min-h-40 font-mono text-[11px]"
              spellCheck={false}
            />
          </Field>
        </>
      )
    case 'recv':
      return (
        <>
          <Field label="request">
            <Input
              value={block.request || ''}
              onChange={(e) => onPatch({ request: e.target.value, response: e.target.value ? '' : block.response })}
              placeholder="INVITE"
            />
          </Field>
          <Field label="response">
            <Input
              value={block.response || ''}
              onChange={(e) => onPatch({ response: e.target.value, request: e.target.value ? '' : block.request })}
              placeholder="200"
            />
          </Field>
          <Field label="timeout">
            <Input value={block.timeout || ''} onChange={(e) => onPatch({ timeout: e.target.value })} />
          </Field>
          <label className="flex items-center gap-2 text-xs">
            <input
              type="checkbox"
              checked={!!block.optional}
              onChange={(e) => onPatch({ optional: e.target.checked })}
            />
            optional
          </label>
        </>
      )
    case 'pause':
      return (
        <Field label="milliseconds">
          <Input
            type="number"
            min={0}
            value={Number(block.milliseconds) || 0}
            onChange={(e) => onPatch({ milliseconds: Number(e.target.value) })}
          />
        </Field>
      )
    case 'label':
      return (
        <Field label="id">
          <Input value={block.label || ''} onChange={(e) => onPatch({ label: e.target.value })} />
        </Field>
      )
    case 'raw':
      return (
        <Field label="XML (preserved)">
          <Textarea
            value={block.xml || ''}
            onChange={(e) => onPatch({ xml: e.target.value })}
            className="min-h-32 font-mono text-[11px]"
            spellCheck={false}
          />
        </Field>
      )
    default:
      return <p className="text-muted-foreground text-xs">No extra fields.</p>
  }
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="flex flex-col gap-1 text-xs">
      <span className="text-muted-foreground">{label}</span>
      {children}
    </label>
  )
}

export function ScenarioGraphEditor({ xml, onChange }: { xml: string; onChange: (xml: string) => void }) {
  const [blocks, setBlocks] = useState(() => xmlToBlocks(xml))
  const [selected, setSelected] = useState(0)
  const [tab, setTab] = useState<'add' | 'edit'>('add')
  const [snap, setSnap] = useState(true)
  const [pos, setPos] = useState(() => defaultLayout(xmlToBlocks(xml).length))
  const dragRef = useRef<{ i: number; ox: number; oy: number } | null>(null)
  const boardRef = useRef<HTMLDivElement>(null)
  const lastEmit = useRef(xml)
  const selectedBlock = blocks[selected]
  const canvasH = Math.max(420, (pos[pos.length - 1]?.y || 0) + NODE_H + 80)

  useEffect(() => {
    if (xml === lastEmit.current) return
    const next = xmlToBlocks(xml)
    setBlocks(next)
    setSelected(0)
    lastEmit.current = xml
    setPos(defaultLayout(next.length))
  }, [xml])

  const emit = (next: GraphBlock[]) => {
    const chainChanged =
      next.length !== blocks.length || next.some((b, i) => b.key !== blocks[i]?.key)
    setBlocks(next)
    if (chainChanged) setPos(defaultLayout(next.length))
    const out = blocksToXML(next)
    lastEmit.current = out
    onChange(out)
  }

  const onPalette = (type: GraphBlockType) => {
    if (type === 'start') {
      setSelected(0)
      setTab('edit')
      return
    }
    if (!canAddBlock(blocks, type)) return
    const next = addBlock(blocks, type)
    emit(next)
    setSelected(next.length - 1)
    setTab('edit')
  }

  const resetLayout = () => setPos(defaultLayout(blocks.length))

  const onCanvasDrop = (e: DragEvent) => {
    e.preventDefault()
    const type = e.dataTransfer.getData(MIME) as GraphBlockType
    if (type) onPalette(type)
  }

  const onNodePointerDown = (e: PointerEvent, i: number) => {
    if (e.button !== 0) return
    setSelected(i)
    setTab('edit')
    const start = pos[i] || { x: NODE_X, y: NODE_Y0 }
    const board = boardRef.current
    const br = board ? board.getBoundingClientRect() : { left: 0, top: 0 }
    const sl = board ? board.scrollLeft : 0
    const st = board ? board.scrollTop : 0
    dragRef.current = {
      i,
      ox: e.clientX - br.left - start.x + sl,
      oy: e.clientY - br.top - start.y + st,
    }
    e.currentTarget.setPointerCapture(e.pointerId)
  }

  const onNodePointerMove = (e: PointerEvent) => {
    const d = dragRef.current
    if (!d) return
    const board = boardRef.current
    const br = board ? board.getBoundingClientRect() : { left: 0, top: 0 }
    const sl = board ? board.scrollLeft : 0
    const st = board ? board.scrollTop : 0
    setPos((prev) => {
      const next = prev.slice()
      next[d.i] = clampPos(e.clientX - br.left - d.ox + sl, e.clientY - br.top - d.oy + st, snap)
      return next
    })
  }

  const onNodePointerUp = () => {
    const d = dragRef.current
    dragRef.current = null
    if (!d || !snap) return
    setPos((prev) => {
      const p = prev[d.i]
      if (!p) return prev
      const next = prev.slice()
      next[d.i] = clampPos(p.x, p.y, true)
      return next
    })
  }

  const palette = useMemo(() => BLOCK_TYPES.filter((t) => t.type !== 'start'), [])

  return (
    <div className="border-border flex min-h-0 flex-1 overflow-hidden rounded-md border">
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="border-border bg-muted/30 flex flex-wrap items-center gap-2 border-b px-2 py-1.5">
          <Button type="button" variant="outline" size="xs" onClick={resetLayout}>
            Reset
          </Button>
          <Button
            type="button"
            size="xs"
            variant={snap ? 'default' : 'outline'}
            onClick={() => setSnap((v) => !v)}
            title={`Snap to ${GRID}px grid`}
          >
            Snap
          </Button>
          <span className="text-muted-foreground text-[11px]">
            Linear SIP XML — drag from Add. Nested actions stay as raw nodes.
          </span>
        </div>
        <div
          ref={boardRef}
          className={cn('relative min-h-0 flex-1 overflow-auto', snap && 'lab-grid')}
          style={snap ? ({ ['--lab-grid']: `${GRID}px` } as CSSProperties) : undefined}
          onDragOver={(e) => e.preventDefault()}
          onDrop={onCanvasDrop}
        >
          <svg className="text-muted-foreground pointer-events-none block overflow-visible" width="100%" height={canvasH}>
            <defs>
              <marker
                id="gp-lab-arrow"
                viewBox="0 0 10 10"
                refX="9"
                refY="5"
                markerWidth="7"
                markerHeight="7"
                orient="auto-start-reverse"
              >
                <path d="M 0 0 L 10 5 L 0 10 z" fill="currentColor" />
              </marker>
            </defs>
            {blocks.slice(0, -1).map((_, i) => {
              const a = pos[i]
              const b = pos[i + 1]
              if (!a || !b) return null
              const { d, mx, my } = edgeGeometry(a, b)
              const label = edgeLabel(blocks[i], blocks[i + 1])
              return (
                <g key={`e-${i}`}>
                  <path d={d} fill="none" stroke="currentColor" strokeWidth="1.6" markerEnd="url(#gp-lab-arrow)" />
                  {label ? (
                    <text
                      x={mx}
                      y={my}
                      textAnchor="middle"
                      dominantBaseline="middle"
                      className="fill-foreground text-[11px] font-semibold"
                      stroke="var(--background)"
                      strokeWidth="4"
                      paintOrder="stroke fill"
                    >
                      {label}
                    </text>
                  ) : null}
                </g>
              )
            })}
          </svg>
          {blocks.map((block, i) => {
            const p = pos[i] || { x: NODE_X, y: NODE_Y0 + i * (NODE_H + NODE_GAP) }
            const meta = blockMeta(block.type)
            return (
              <div
                key={block.key}
                className={cn(
                  'bg-card border-border absolute flex cursor-grab touch-none items-center gap-2 rounded-[10px] border px-2.5 py-2 shadow-sm select-none',
                  i === selected && 'border-primary ring-primary/30 ring-2',
                )}
                style={{ left: p.x, top: p.y, width: NODE_W, height: NODE_H }}
                onPointerDown={(e) => onNodePointerDown(e, i)}
                onPointerMove={onNodePointerMove}
                onPointerUp={onNodePointerUp}
                role="button"
                tabIndex={0}
              >
                <span className="border-border text-foreground flex size-9 shrink-0 items-center justify-center rounded-md border">
                  <BlockIcon type={block.type} size={22} />
                </span>
                <span className="flex min-w-0 flex-col">
                  <strong className="text-[13px]">{meta.label}</strong>
                  <em className="text-muted-foreground truncate text-[11px] not-italic">{cardHint(block)}</em>
                </span>
              </div>
            )
          })}
        </div>
      </div>

      <aside className="border-border bg-card flex w-72 shrink-0 flex-col border-l">
        <div className="border-border flex border-b">
          {(['add', 'edit'] as const).map((id) => (
            <button
              key={id}
              type="button"
              className={cn(
                'flex-1 px-2 py-2 text-xs font-semibold',
                tab === id ? 'text-foreground shadow-[inset_0_-2px_0_var(--primary)]' : 'text-muted-foreground',
              )}
              onClick={() => setTab(id)}
              disabled={id === 'edit' && !selectedBlock}
            >
              {id === 'add' ? 'Add' : 'Edit'}
            </button>
          ))}
        </div>
        <div className="min-h-0 flex-1 overflow-auto p-3">
          {tab === 'add' ? (
            <div className="flex flex-col gap-0.5">
              {palette.map((t) => (
                <div
                  key={t.type}
                  className="hover:bg-muted flex min-h-10 cursor-grab items-center gap-2 rounded-md px-1.5 py-1"
                  draggable
                  onDragStart={(e) => {
                    e.dataTransfer.setData(MIME, t.type)
                    e.dataTransfer.effectAllowed = 'copy'
                  }}
                  onClick={() => onPalette(t.type)}
                >
                  <span className="border-border flex size-9 items-center justify-center rounded-md border">
                    <BlockIcon type={t.type} size={20} />
                  </span>
                  <span className="text-sm">{t.label}</span>
                </div>
              ))}
            </div>
          ) : selectedBlock ? (
            <div className="flex flex-col gap-2">
              <div className="flex items-center gap-2">
                <BlockIcon type={selectedBlock.type} size={20} />
                <strong className="text-sm">{blockMeta(selectedBlock.type).label}</strong>
                {selectedBlock.type !== 'start' ? (
                  <Button
                    type="button"
                    variant="destructive"
                    size="xs"
                    className="ml-auto"
                    onClick={() => {
                      const next = removeBlock(blocks, selected)
                      emit(next)
                      setSelected(Math.max(0, selected - 1))
                    }}
                  >
                    Remove
                  </Button>
                ) : null}
              </div>
              {selectedBlock.type !== 'start' ? (
                <div className="flex gap-1">
                  <Button
                    type="button"
                    variant="outline"
                    size="xs"
                    onClick={() => emit(moveBlock(blocks, selected, -1))}
                  >
                    Up
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="xs"
                    onClick={() => emit(moveBlock(blocks, selected, 1))}
                  >
                    Down
                  </Button>
                </div>
              ) : null}
              <BlockFields
                block={selectedBlock}
                onPatch={(partial) => {
                  emit(blocks.map((b, i) => (i === selected ? { ...b, ...partial } : b)))
                }}
              />
            </div>
          ) : null}
        </div>
      </aside>
    </div>
  )
}
