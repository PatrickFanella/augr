import { QueryClient } from '@tanstack/react-query'

import { isApiClientError } from '@/shared/api/errors'

export function createAppQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 30_000,
        refetchOnWindowFocus: false,
        retry(failureCount, error) {
          if (isApiClientError(error)) {
            if (['unauthorized', 'validation', 'conflict', 'not_found', 'not_implemented'].includes(error.kind)) return false
            if (error.status && error.status >= 500) return failureCount < 1
          }
          return failureCount < 1
        },
      },
      mutations: { retry: false },
    },
  })
}
