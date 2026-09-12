// A drag-and-drop (or click-to-browse) file picker, shared by the two
// places issue #214 needs one: the import wizard's upload step
// (pages/data/Import.tsx) and the restore flow's backup-file picker
// (pages/data/Restore.tsx). shadcn/ui's default registry — the only one
// this project's components.json points at — has no dropzone/file-upload
// primitive to pull instead (checked against the registry directly, not
// assumed), so this is a small, boring, explicit component rather than a
// missing "pull it via shadcn" step.
//
// A plain <label htmlFor={inputId}> wrapping a visually-hidden <input
// type="file"> is what makes the whole box clickable without any manual
// click-forwarding ref/JS — that's standard label/control association,
// not a custom behavior this component invents. Drag-and-drop is layered
// on top of the same hidden input: a drop just calls the same onFile the
// input's own onChange does, so there is exactly one code path for "a
// file was chosen," regardless of how.
import { UploadIcon, type LucideIcon } from 'lucide-react'
import { useId, useState, type DragEvent } from 'react'

import { cn } from '@/lib/utils'

export type FileDropzoneProps = {
  id?: string
  accept?: string
  // The chosen file's own name, for display — a name string rather than
  // the File object itself, since a caller that has already read and
  // parsed the file (Restore.tsx) has no reason to keep holding onto the
  // File after that, only what to show for it.
  fileName?: string
  onFileChange: (file: File | null) => void
  // What kind of file this expects, shown as the box's own prompt (e.g.
  // "CSV file" / "JSON backup file") — callers differ enough here
  // (Import's CSV vs. Restore's JSON) that a fixed default would be
  // wrong for one of them.
  prompt: string
  hint?: string
  disabled?: boolean
  icon?: LucideIcon
}

export function FileDropzone({
  id,
  accept,
  fileName,
  onFileChange,
  prompt,
  hint,
  disabled = false,
  icon: Icon = UploadIcon,
}: FileDropzoneProps) {
  const generatedId = useId()
  const inputId = id ?? generatedId
  const [dragActive, setDragActive] = useState(false)

  function handleDrop(event: DragEvent<HTMLLabelElement>) {
    event.preventDefault()
    setDragActive(false)
    if (disabled) return
    const dropped = event.dataTransfer.files?.[0]
    if (dropped) onFileChange(dropped)
  }

  return (
    <label
      htmlFor={inputId}
      data-active={dragActive}
      data-disabled={disabled}
      onDragOver={(event) => {
        event.preventDefault()
        if (!disabled) setDragActive(true)
      }}
      onDragLeave={() => setDragActive(false)}
      onDrop={handleDrop}
      className={cn(
        'border-input flex flex-col items-center justify-center gap-1.5 rounded-lg border-2 border-dashed px-6 py-8 text-center transition-colors',
        !disabled && 'hover:border-primary/50 hover:bg-muted/40 cursor-pointer',
        dragActive && 'border-primary bg-muted/60',
        disabled && 'cursor-not-allowed opacity-50',
      )}
    >
      <input
        id={inputId}
        type="file"
        accept={accept}
        disabled={disabled}
        className="sr-only"
        onChange={(event) => onFileChange(event.target.files?.[0] ?? null)}
      />
      <Icon
        className={cn(
          'size-6',
          fileName ? 'text-foreground' : 'text-muted-foreground',
        )}
      />
      {fileName ? (
        <p className="text-sm font-medium">{fileName}</p>
      ) : (
        <>
          <p className="text-sm">
            <span className="text-foreground font-medium">Click to upload</span>{' '}
            <span className="text-muted-foreground">or drag and drop</span>
          </p>
          <p className="text-muted-foreground text-xs">{prompt}</p>
        </>
      )}
      {hint && <p className="text-muted-foreground text-xs">{hint}</p>}
    </label>
  )
}
