import { RouterProvider } from 'react-router-dom'
import { useMemo } from 'react'

import { AppProviders } from '@/app/providers/AppProviders'
import { createAppRouter } from '@/app/router/router'

function App() {
  const router = useMemo(() => createAppRouter(), [])
  return (
    <AppProviders>
      <RouterProvider router={router} />
    </AppProviders>
  )
}

export default App
