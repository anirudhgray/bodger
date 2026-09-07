// The Password subpage of Settings (issue #107, split out of the old
// Settings.tsx). No behavior change from the original PasswordSection —
// only its own route now, rendered inside SettingsLayout's <Outlet />.
import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { changePassword } from '@/lib/settings'
import { errorMessage } from './shared'

export function PasswordSettings() {
  const navigate = useNavigate()
  const [newPassword, setNewPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [submitting, setSubmitting] = useState(false)

  // Changing the password is a user-triggered action (docs/design-
  // system.md's "Toasts vs. inline messages"), so every failure of this
  // submit — the client-side mismatch check included — reports via a
  // toast rather than inline; there's no load state on this page at all.
  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (newPassword !== confirm) {
      toast.error('Passwords didn’t match.')
      return
    }
    setSubmitting(true)
    try {
      await changePassword(newPassword)
      // changePassword revokes every session, including this one
      // (internal/surface/http/auth.go's changePassword doc comment) -
      // the browser's cookie is already dead, so head to login rather
      // than staying on a page that will 401 on its next request.
      navigate('/login', { replace: true })
    } catch (err) {
      toast.error(errorMessage(err))
      setSubmitting(false)
    }
  }

  return (
    <section className="flex flex-col gap-4">
      <p className="text-muted-foreground text-sm">
        Changing your password signs you out everywhere, including this browser.
      </p>
      <form
        className="flex max-w-xs flex-col gap-4"
        onSubmit={handleSubmit}
        noValidate
      >
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="new-password">New password</Label>
          <Input
            id="new-password"
            type="password"
            autoComplete="new-password"
            required
            value={newPassword}
            onChange={(event) => setNewPassword(event.target.value)}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="confirm-password">Confirm new password</Label>
          <Input
            id="confirm-password"
            type="password"
            autoComplete="new-password"
            required
            value={confirm}
            onChange={(event) => setConfirm(event.target.value)}
          />
        </div>
        <Button
          type="submit"
          disabled={submitting || newPassword === '' || confirm === ''}
          className="self-start"
        >
          {submitting ? 'Changing…' : 'Change password'}
        </Button>
      </form>
    </section>
  )
}
