import { useState, type FormEvent } from 'react'
import { Navigate, useNavigate, useSearchParams } from 'react-router-dom'

import { Alert } from '@/components/ui/alert'
import { isApiClientError } from '@/shared/api/errors'
import { useAuth } from '@/shared/auth/AuthProvider'

function safeNext(value: string | null): string {
  if (!value || !value.startsWith('/') || value.startsWith('//')) return '/cockpit'
  if (value === '/login' || value.startsWith('/login?')) return '/cockpit'
  return value
}

export function LoginPage() {
  const auth = useAuth()
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  if (auth.status === 'authenticated') return <Navigate to={safeNext(params.get('next'))} replace />

  async function onSubmit(event: FormEvent) {
    event.preventDefault()
    setError(null)
    setSubmitting(true)
    try {
      await auth.login({ username, password })
      navigate(safeNext(params.get('next')), { replace: true })
    } catch (err) {
      setError(isApiClientError(err) && err.kind === 'unauthorized' ? 'Invalid username or password.' : 'Unable to sign in. Try again.')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <main className="login-shell">
      <form className="login-card" onSubmit={onSubmit} aria-label="Sign in">
        <p className="eyebrow">Augr operations</p>
        <h1>Sign in</h1>
        {params.get('reason') === 'expired' ? <Alert variant="warning">Session expired. Sign in to continue to your previous destination.</Alert> : null}
        <label>
          Username
          <input autoComplete="username" value={username} onChange={(event) => setUsername(event.target.value)} required />
        </label>
        <label>
          Password
          <input type="password" autoComplete="current-password" value={password} onChange={(event) => setPassword(event.target.value)} required />
        </label>
        {error ? <Alert variant="danger">{error}</Alert> : null}
        <button type="submit" disabled={submitting}>{submitting ? 'Signing in…' : 'Sign in'}</button>
      </form>
    </main>
  )
}
