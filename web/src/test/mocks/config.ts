export type MockMode = 'off' | 'test' | 'development'

export type MockConfig = {
  mode: MockMode
  apiBaseUrl: string
}

type MockEnv = {
  MODE?: string
  PROD?: boolean
  VITE_ENABLE_API_MOCKS?: string
  VITE_API_BASE_URL?: string
}

export function resolveMockConfig(env: MockEnv = import.meta.env): MockConfig {
  const explicit = env.VITE_ENABLE_API_MOCKS === 'true'
  const isTest = env.MODE === 'test'
  const isProd = env.PROD === true || env.MODE === 'production'

  if (isProd && explicit) {
    throw new Error('API mocks cannot be enabled in production builds')
  }

  if (isTest) {
    return { mode: 'test', apiBaseUrl: env.VITE_API_BASE_URL ?? '/api/v1' }
  }

  if (explicit) {
    return { mode: 'development', apiBaseUrl: env.VITE_API_BASE_URL ?? '/api/v1' }
  }

  return { mode: 'off', apiBaseUrl: env.VITE_API_BASE_URL ?? '/api/v1' }
}

export function shouldEnableMocks(env: MockEnv = import.meta.env): boolean {
  return resolveMockConfig(env).mode !== 'off'
}
