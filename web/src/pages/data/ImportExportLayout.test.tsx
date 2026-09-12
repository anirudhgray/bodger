// Component tests for the Import & export tab strip (issue #214),
// mirroring settings/SettingsLayout.test.tsx's own coverage: the three
// tabs are real links to their own routes, the tab matching the current
// URL is marked active, and clicking another tab actually navigates
// there.
import { render, screen } from '@testing-library/react'
import { createMemoryRouter, RouterProvider } from 'react-router-dom'
import { describe, expect, it } from 'vitest'

import { ImportExportLayout } from './ImportExportLayout'

function renderAt(path: string) {
  const router = createMemoryRouter(
    [
      {
        path: '/import-export',
        element: <ImportExportLayout />,
        children: [
          { path: 'import', element: <div>Import page</div> },
          { path: 'export', element: <div>Export page</div> },
          { path: 'restore', element: <div>Restore page</div> },
        ],
      },
    ],
    { initialEntries: [path] },
  )
  render(<RouterProvider router={router} />)
  return router
}

describe('ImportExportLayout', () => {
  it('renders a tab per section, each a real link to its own route', () => {
    renderAt('/import-export/import')

    for (const [name, href] of [
      ['Import', '/import-export/import'],
      ['Export', '/import-export/export'],
      ['Restore', '/import-export/restore'],
    ]) {
      const link = screen.getByRole('tab', { name })
      expect(link.tagName).toBe('A')
      expect(link).toHaveAttribute('href', href)
    }
  })

  it('marks the tab matching the current URL as selected', async () => {
    renderAt('/import-export/export')

    expect(await screen.findByText('Export page')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Export' })).toHaveAttribute(
      'aria-selected',
      'true',
    )
    expect(screen.getByRole('tab', { name: 'Import' })).toHaveAttribute(
      'aria-selected',
      'false',
    )
  })

  it('navigates to another subpage when its tab is clicked', async () => {
    renderAt('/import-export/import')
    expect(await screen.findByText('Import page')).toBeInTheDocument()

    screen.getByRole('tab', { name: 'Restore' }).click()

    expect(await screen.findByText('Restore page')).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: 'Restore' })).toHaveAttribute(
      'aria-selected',
      'true',
    )
  })
})
