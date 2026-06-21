import { z } from 'zod'

const envSchema = z.object({
  VITE_API_BASE_URL: z.string().default('/api/v1'),
  VITE_WS_BASE_URL: z.string().default('/ws'),
  VITE_ENABLE_API_MOCKS: z.enum(['true', 'false']).default('false'),
})

const parsed = envSchema.parse(import.meta.env)

export const appConfig = {
  apiBaseUrl: parsed.VITE_API_BASE_URL,
  wsBaseUrl: parsed.VITE_WS_BASE_URL,
  enableApiMocks: parsed.VITE_ENABLE_API_MOCKS === 'true',
}
