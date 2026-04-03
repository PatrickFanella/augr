import { type FormEvent, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'

import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { apiClient, ApiClientError } from '@/lib/api/client'
import { setTokens } from '@/lib/auth'

export function LoginPage() {
  const navigate = useNavigate()
  const location = useLocation()

  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  async function handleSubmit(e: FormEvent) {
    e.preventDefault()
    setError(null)

    if (!username.trim() || !password.trim()) {
      setError('Username and password are required')
      return
    }

    setLoading(true)

    try {
      const res = await apiClient.login({ username, password })
      setTokens(res.access_token, res.refresh_token, new Date(res.expires_at).getTime())
      const redirectTo = (location.state as { from?: string } | null)?.from ?? '/'
      navigate(redirectTo, { replace: true })
    } catch (err) {
      if (err instanceof ApiClientError) {
        setError(err.status === 401 ? 'Invalid username or password' : err.message)
      } else {
        setError('Unable to connect to server')
      }
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="mx-auto grid min-h-screen max-w-6xl gap-6 px-4 py-8 lg:grid-cols-[minmax(0,1.1fr)_420px] lg:items-center">
      <section className="hidden rounded-2xl border border-white/10 bg-card/80 p-8 shadow-[0_0_0_1px_rgba(255,255,255,0.02),0_24px_60px_rgba(2,6,23,0.35)] backdrop-blur-sm lg:block">
        <p className="font-mono text-[11px] uppercase tracking-[0.24em] text-primary/85">Get Rich Quick</p>
        <h1 className="mt-4 max-w-xl text-4xl font-semibold tracking-tight text-foreground">
          Dark-mode trading operations, without the wasted pixels.
        </h1>
        <p className="mt-4 max-w-2xl text-base leading-7 text-muted-foreground">
          Monitor strategies, inspect pipeline runs, manage risk controls, and keep portfolio state in view from one dense operator console.
        </p>
        <div className="mt-8 grid gap-3 sm:grid-cols-2">
          {[
            ['Strategies', 'Create, schedule, pause, and run systems from one surface.'],
            ['Pipeline runs', 'Inspect agent decisions, debate rounds, and final signals live.'],
            ['Risk controls', 'Circuit breakers, kill switch controls, and utilization telemetry.'],
            ['Portfolio state', 'Open positions, trades, and realized performance in one place.'],
          ].map(([title, copy]) => (
            <div key={title} className="rounded-lg border border-white/8 bg-background/70 p-4">
              <p className="font-mono text-[11px] uppercase tracking-[0.18em] text-primary/75">{title}</p>
              <p className="mt-2 text-sm leading-6 text-muted-foreground">{copy}</p>
            </div>
          ))}
        </div>
      </section>

      <Card className="w-full">
        <CardHeader>
          <CardTitle>Sign in</CardTitle>
          <CardDescription>
            Authenticate to access the trading command center.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSubmit} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="username">Username</Label>
              <Input
                id="username"
                type="text"
                autoComplete="username"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                disabled={loading}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="password">Password</Label>
              <Input
                id="password"
                type="password"
                autoComplete="current-password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                disabled={loading}
              />
            </div>
            {error && <p className="text-sm text-destructive" role="alert">{error}</p>}
            <Button type="submit" className="w-full" disabled={loading}>
              {loading ? 'Signing in...' : 'Sign in'}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
