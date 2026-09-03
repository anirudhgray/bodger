// The real login screen (issue #59), replacing routes.tsx's placeholder.
// A single password field — there's no username: the REST API's login
// route authenticates the one seeded user by password alone
// (internal/surface/http/auth.go's loginRequest doc comment).
import { useState, type FormEvent } from 'react'
import { useNavigate } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ApiError } from '@/lib/api'
import { login } from '@/lib/session'

export function Login() {
  const navigate = useNavigate()
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      await login(password)
      navigate('/', { replace: true })
    } catch (err) {
      setError(
        err instanceof ApiError
          ? err.message
          : 'Couldn’t reach the server. Try again.',
      )
      setSubmitting(false)
    }
  }

  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-6 p-6">
      <div className="flex w-full max-w-xs flex-col gap-6">
        <div className="flex flex-col items-center gap-1 text-center">
          <span className="text-sm font-semibold tracking-tight">bodger</span>
          <h1 className="text-2xl font-semibold tracking-tight">Log in</h1>
        </div>
        <form
          className="flex flex-col gap-4"
          onSubmit={handleSubmit}
          noValidate
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="password">Password</Label>
            <Input
              id="password"
              name="password"
              type="password"
              autoComplete="current-password"
              autoFocus
              required
              aria-invalid={error !== null}
              value={password}
              onChange={(event) => setPassword(event.target.value)}
            />
          </div>
          {error && (
            <p role="alert" className="text-destructive text-sm">
              {error}
            </p>
          )}
          <Button type="submit" disabled={submitting || password === ''}>
            {submitting ? 'Logging in…' : 'Log in'}
          </Button>
        </form>
      </div>
    </div>
  )
}
