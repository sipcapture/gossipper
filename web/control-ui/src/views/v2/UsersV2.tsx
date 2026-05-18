import { useCallback, useEffect, useMemo, useState } from 'react'

import { createUser, deleteUser, listUsers, updateUser, type User } from '@/api/v2'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { DataTable, type Column } from '@/components/ui/data-table'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Modal } from '@/components/ui/modal'
import { useToast } from '@/lib/toast'

export type UsersV2Props = {
  bearer?: string
  busy: boolean
  run: <T>(fn: () => Promise<T>) => Promise<T | undefined>
  errorText?: string | null
}

type CreateDraft = { username: string; password: string; role: string }
type EditDraft = { id: number; username: string; password: string; role: string }

export function UsersV2({ bearer, busy, run, errorText }: UsersV2Props) {
  const { toast } = useToast()
  const [users, setUsers] = useState<User[]>([])
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

  const onCreate = () => {
    if (!createDraft) return
    void run(async () => {
      await createUser(
        { username: createDraft.username, password: createDraft.password, role: createDraft.role || undefined },
        { bearer },
      )
      setCreateDraft(null)
      toast('User created', 'success')
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
      toast('User updated', 'success')
      await refresh()
    })
  }

  const onDelete = (u: User) => {
    if (!window.confirm(`Delete user "${u.username}"?`)) return
    void run(async () => {
      await deleteUser(u.id, { bearer })
      toast('User deleted', 'success')
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
            Admin accounts that can sign in to the console (<code>users</code> table in{' '}
            <code>settings.sqlite</code>). Audit entries are on the <strong>Audit</strong> page.
          </p>
        </div>
        <div className="flex gap-2">
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
    </section>
  )
}
