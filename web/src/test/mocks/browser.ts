import { setupWorker } from 'msw/browser'

import { createP0RestHandlers } from '@/test/mocks/rest'

export const worker = setupWorker(...createP0RestHandlers())

export async function startApiMocks() {
  await worker.start({ onUnhandledRequest: 'bypass' })
}
