import { QueryClientProvider } from '@tanstack/react-query'
import { useState, type ReactNode } from 'react'

import { appConfig } from '@/app/config/env'
import { configureApiClient } from '@/shared/api/client'
import { AuthProvider, useAuth } from '@/shared/auth/AuthProvider'
import { ThemeProvider } from '@/app/providers/ThemeProvider'
import { createAppQueryClient } from '@/shared/query/client'
import { RealtimeProvider } from '@/shared/websocket/RealtimeProvider'

configureApiClient({ baseUrl: appConfig.apiBaseUrl })

function RealtimeBridge({ children }: { children: ReactNode }) {
  const auth = useAuth()
  return <RealtimeProvider authenticated={auth.status === 'authenticated'}>{children}</RealtimeProvider>
}

export function AppProviders({ children }: { children: ReactNode }) {
  const [queryClient] = useState(() => createAppQueryClient())
  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <AuthProvider>
          <RealtimeBridge>{children}</RealtimeBridge>
        </AuthProvider>
      </ThemeProvider>
    </QueryClientProvider>
  )
}
