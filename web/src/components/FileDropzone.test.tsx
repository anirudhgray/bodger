// Component tests for FileDropzone (issue #214's shared upload widget,
// used by both the import wizard and the restore flow): the hidden input
// still receives a click-to-browse selection, a drop delivers a file the
// same way, and the chosen file's name renders once picked.
import { fireEvent, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'

import { FileDropzone } from './FileDropzone'

function csvFile(name = 'statement.csv') {
  return new File(['a,b\n1,2'], name, { type: 'text/csv' })
}

describe('FileDropzone', () => {
  it('calls onFileChange when a file is chosen via the hidden input', async () => {
    const user = userEvent.setup()
    const onFileChange = vi.fn()
    const file = csvFile()
    render(
      <FileDropzone
        id="test-file"
        onFileChange={onFileChange}
        prompt="A CSV file"
      />,
    )

    await user.upload(screen.getByLabelText(/click to upload/i), file)

    expect(onFileChange).toHaveBeenCalledWith(file)
  })

  it('shows the chosen file name instead of the prompt', () => {
    render(
      <FileDropzone
        id="test-file"
        fileName="statement.csv"
        onFileChange={vi.fn()}
        prompt="A CSV file"
      />,
    )

    expect(screen.getByText('statement.csv')).toBeInTheDocument()
    expect(screen.queryByText('A CSV file')).not.toBeInTheDocument()
  })

  it('delivers a dropped file to onFileChange', () => {
    const onFileChange = vi.fn()
    const file = csvFile('dropped.csv')
    render(
      <FileDropzone
        id="test-file"
        onFileChange={onFileChange}
        prompt="A CSV file"
      />,
    )

    const dropzone = screen.getByLabelText(/click to upload/i)
    fireEvent.drop(dropzone, { dataTransfer: { files: [file] } })

    expect(onFileChange).toHaveBeenCalledWith(file)
  })

  it('does not call onFileChange when disabled', () => {
    const onFileChange = vi.fn()
    render(
      <FileDropzone
        id="test-file"
        onFileChange={onFileChange}
        prompt="A CSV file"
        disabled
      />,
    )

    fireEvent.drop(screen.getByText('A CSV file'), {
      dataTransfer: { files: [csvFile()] },
    })

    expect(onFileChange).not.toHaveBeenCalled()
  })
})
