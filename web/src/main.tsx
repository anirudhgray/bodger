import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { RouterProvider } from 'react-router-dom'

import { Toaster } from './components/ui/toast.tsx'
import { ThemeProvider } from './hooks/use-theme.tsx'
import './index.css'
import { router } from './routes.tsx'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ThemeProvider>
      <RouterProvider router={router} />
      <Toaster />
    </ThemeProvider>
  </StrictMode>,
)
