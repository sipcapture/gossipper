import { useCallback, useEffect, useMemo, useState } from 'react'

import {
  createUser,
  deleteUser,
  listAudit,
  listUsers,
  updateUser,
  type AuditEntry,
  type User,
} from '@/api/v2'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { DataTable, type Column } from '@/components/ui/data-table'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Modal } from '@/components/ui/modal'

export type UsersV2Props = {
  bearer?: string
  busy: boolean
  run: <T>(fn: () => Promise<T>) => Promise<T | undefined>
  errorText?: string | null
}

type CreateDraft = { username: string; password: string; role: string }
type EditDraft = { id: number; username: string; password: string; role: string }

export function UsersV2({ bearer, busy, run, errorText }: UsersV2Props) {
  const [users, setUsers] = useState<User[]>([])
  const [audit, setAudit] = useState<AuditEntry[]>([])
  const [auditOpen, setAuditOpen] = useState(false)
  const [auditFilter, setAuditFilter] = useState('')
  const [auditAutoRefresh, setAuditAutoRefresh] = useState(true)
  const [createDraft, setCreateDraft] = useState<CreateDraft | null>(null)
  const [editDraft, setEditDraft] = useState<EditDraft | null>(null)
  const [unauthorized, setUnauthorized] = useState(false)

  const refresh = useCallback(async () => {
    try {
      const r = await listUsers({ bearer })
      setUsers(r.users ?? [])
      setUnauthorized(false)
    } catch (e: unknown) {
      if (e && typeof e === 'object' && 'status' in e && (e as { status: number }).status === 404) {
        setUnauthorized(true)
      } else {
        throw e
      }
    }
  }, [bearer])

  useEffect(() => {
    void run(() => refresh())
  }, [run, refresh])

  const loadAudit = useCallback(async () => {
    const r = await listAudit({ bearer }, 200)
    setAudit(r.audit ?? [])
  }, [bearer])

  const openAudit = () => {
    void run(async () => {
      await loadAudit()
      setAuditOpen(true)
    })
  }

  useEffect(() => {
    if (!auditOpen || !auditAutoRefresh) return
    const tick = setInterval(() => {
      void loadAudit().catch(() => {
        // swallow — modal stays open with stale data.
      })
    }, 4000)
    return () => clearInterval(tick)
  }, [auditOpen, auditAutoRefresh, loadAudit])

  const filteredAudit = useMemo(() => {
    const q = auditFilter.trim().toLowerCase()
    if (!q) return audit
    return audit.filter((e) => {
      return (
        e.action.toLowerCase().includes(q) ||
        (e.username ?? '').toLowerCase().includes(q) ||
        (e.target ?? '').toLowerCase().includes(q)
      )
    })
  }, [audit, auditFilter])

  const onCreate = () => {
    if (!createDraft) return
    void run(async () => {
      await createUser(
        { username: createDraft.username, password: createDraft.password, role: createDraft.role || undefined },
        { bearer },
      )
      setCreateDraft(null)
      await refresh()
    })
  }

  const onSaveEdit = () => {
    if (!editDraft) return
    void run(async () => {
      await updateUser(
        editDraft.id,
        { password: editDraft.password || undefined, role: editDraft.role || undefined },
        { bearer },
      )
      setEditDraft(null)
      await refresh()
    })
  }

  const onDelete = (u: User) => {
    if (!window.confirm(`Delete user "${u.username}"?`)) return
    void run(async () => {
      await deleteUser(u.id, { bearer })
      await refresh()
    })
  }

  const columns: Column<User>[] = useMemo(
    () => [
      { key: 'id', header: 'ID', render: (r) => <code className="text-xs">{r.id}</code> },
      { key: 'username', header: 'Username', render: (r) => r.username },
      { key: 'role', header: 'Role', render: (r) => r.role || '—' },
      { key: 'created', header: 'Created', render: (r) => new Date(r.created_at).toLocaleString() },
      {
        key: 'actions',
        header: '',
        align: 'right',
        render: (r) => (
          <div className="flex justify-end gap-1">
            <Button
              type="button"
              variant="outline"
              size="xs"
              onClick={() => setEditDraft({ id: r.id, username: r.username, password: '', role: r.role || 'admin' })}
            >
              Edit
            </Button>
            <Button type="button" variant="destructive" size="xs" onClick={() => onDelete(r)}>
              Delete
            </Button>
          </div>
        ),
      },
    ],
    [onDelete],
  )

  if (unauthorized) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Users</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-muted-foreground text-xs">
            User management requires <code>auth.type: internal</code>. The server reported the users
            API as disabled.
          </p>
        </CardContent>
      </Card>
    )
  }

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 className="text-sm font-semibold">Users</h2>
          <p className="text-muted-foreground text-xs">
            Admin accounts that can sign in to the console (<code>users</code> table in
            <code>settings.sqlite</code>). The role column is reserved for future RBAC tiers.
          </p>
        </div>
        <div className="flex gap-2">
          <Button type="button" variant="outline" size="sm" onClick={openAudit}>
            Audit log
          </Button>
          <Button type="button" variant="outline" size="sm" onClick={() => void run(() => refresh())}>
            Refresh
          </Button>
          <Button
            type="button"
            size="sm"
            onClick={() => setCreateDraft({ username: '', password: '', role: 'admin' })}
          >
            + New user
          </Button>
        </div>
      </div>

      {errorText ? (
        <div className="border-destructive/40 bg-destructive/10 text-destructive rounded-md border px-3 py-2 text-xs">
          {errorText}
        </div>
      ) : null}

      <DataTable
        rows={users}
        columns={columns}
        rowKey={(r) => String(r.id)}
        loading={busy && users.length === 0}
        empty="No users yet."
      />

      <Modal
        open={createDraft !== null}
        onClose={() => setCreateDraft(null)}
        size="sm"
        title="New user"
        footer={
          <>
            <Button type="button" variant="outline" size="sm" onClick={() => setCreateDraft(null)}>
              Cancel
            </Button>
            <Button type="button" size="sm" onClick={onCreate} disabled={busy}>
              Create
            </Button>
          </>
        }
      >
        {createDraft ? (
          <div className="flex flex-col gap-3">
            <div>
              <Label className="text-xs">Username</Label>
              <Input
                value={createDraft.username}
                onChange={(e) => setCreateDraft({ ...createDraft, username: e.target.value })}
                className="mt-1"
              />
            </div>
            <div>
              <Label className="text-xs">Password (min 8 chars)</Label>
              <Input
                type="password"
                value={createDraft.password}
                onChange={(e) => setCreateDraft({ ...createDraft, password: e.target.value })}
                className="mt-1"
              />
            </div>
            <div>
              <Label className="text-xs">Role</Label>
              <Input
                value={createDraft.role}
                onChange={(e) => setCreateDraft({ ...createDraft, role: e.target.value })}
                className="mt-1"
                placeholder="admin"
              />
            </div>
          </div>
        ) : null}
      </Modal>

      <Modal
        open={editDraft !== null}
        onClose={() => setEditDraft(null)}
        size="sm"
        title={editDraft ? `Edit ${editDraft.username}` : 'Edit'}
        footer={
          <>
            <Button type="button" variant="outline" size="sm" onClick={() => setEditDraft(null)}>
              Cancel
            </Button>
            <Button type="button" size="sm" onClick={onSaveEdit} disabled={busy}>
              Save
            </Button>
          </>
        }
      >
        {editDraft ? (
          <div className="flex flex-col gap-3">
            <div>
              <Label className="text-xs">New password (leave blank to keep)</Label>
              <Input
                type="password"
                value={editDraft.password}
                onChange={(e) => setEditDraft({ ...editDraft, password: e.target.value })}
                className="mt-1"
                placeholder="min 8 chars"
              />
            </div>
            <div>
              <Label className="text-xs">Role</Label>
              <Input
                value={editDraft.role}
                onChange={(e) => setEditDraft({ ...editDraft, role: e.target.value })}
                className="mt-1"
              />
            </div>
          </div>
        ) : null}
      </Modal>

      <Modal
        open={auditOpen}
        onClose={() => setAuditOpen(false)}
        size="lg"
        title="Audit log"
        description="Most recent mutating actions performed via /api/v2 (limit 200)."
      >
        <div className="mb-2 flex flex-wrap items-center justify-between gap-2">
          <Input
            value={auditFilter}
            onChange={(e) => setAuditFilter(e.target.value)}
            placeholder="filter by action / user / target…"
            className="h-7 max-w-sm text-xs"
          />
          <div className="flex items-center gap-2">
            <label className="text-muted-foreground flex items-center gap-1 text-[11px]">
              <input
                type="checkbox"
                checked={auditAutoRefresh}
                onChange={(e) => setAuditAutoRefresh(e.target.checked)}
              />
              auto-refresh
            </label>
            <Button
              type="button"
              variant="outline"
              size="xs"
              onClick={() => {
                void run(() => loadAudit())
              }}
            >
              Refresh
            </Button>
            <span className="text-muted-foreground text-[11px]">
              {filteredAudit.length}/{audit.length}
            </span>
          </div>
        </div>
        {filteredAudit.length === 0 ? (
          <p className="text-muted-foreground text-xs">
            {audit.length === 0 ? 'No entries yet.' : 'No entries match the filter.'}
          </p>
        ) : (
          <table className="w-full text-xs">
            <thead className="text-muted-foreground">
              <tr>
                <th className="border-border border-b px-2 py-1 text-left">Time</th>
                <th className="border-border border-b px-2 py-1 text-left">User</th>
                <th className="border-border border-b px-2 py-1 text-left">Action</th>
                <th className="border-border border-b px-2 py-1 text-left">Target</th>
              </tr>
            </thead>
            <tbody>
              {filteredAudit.map((e) => (
                <tr key={e.id} className="border-border border-b">
                  <td className="px-2 py-1 font-mono">{new Date(e.ts).toLocaleString()}</td>
                  <td className="px-2 py-1">{e.username || '—'}</td>
                  <td className="px-2 py-1 font-mono">{e.action}</td>
                  <td className="px-2 py-1 font-mono">{e.target || '—'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </Modal>
    </section>
  )
}
