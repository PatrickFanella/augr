import { describe, expect, it } from 'vitest'

import { resolveMockConfig, shouldEnableMocks } from '@/test/mocks/config'

describe('mock config', () => {
  it('enables mocks explicitly in development', () => {
    expect(shouldEnableMocks({ MODE: 'development', VITE_ENABLE_API_MOCKS: 'true' })).toBe(true)
  })

  it('enables mocks automatically in tests', () => {
    expect(resolveMockConfig({ MODE: 'test' }).mode).toBe('test')
  })

  it('refuses production mocks', () => {
    expect(() => resolveMockConfig({ MODE: 'production', PROD: true, VITE_ENABLE_API_MOCKS: 'true' })).toThrow('production')
  })
})
