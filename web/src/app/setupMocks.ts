export async function setupMocks() {
  if (import.meta.env.PROD) {
    if (import.meta.env.VITE_ENABLE_API_MOCKS === 'true') throw new Error('API mocks cannot be enabled in production builds.')
    return
  }
  const { shouldEnableMocks } = await import('@/test/mocks/config')
  if (!shouldEnableMocks()) return
  const { startApiMocks } = await import('@/test/mocks/browser')
  await startApiMocks()
}
