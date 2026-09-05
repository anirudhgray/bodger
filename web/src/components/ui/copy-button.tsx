import { Check, Copy } from 'lucide-react'
import { useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

const COPIED_RESET_MS = 1500

// A reusable copy-to-clipboard button (an API token, an account number,
// anything shown once) — shows a brief checkmark instead of relying on a
// separate toast/announcement, and resets on its own without leaking a
// timer if the button unmounts first (a confirmation dialog closing).
export function CopyButton({
  value,
  label = 'Copy',
  className,
  ...props
}: {
  value: string
  label?: string
} & Omit<
  React.ComponentProps<typeof Button>,
  'onClick' | 'children' | 'aria-label'
>) {
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    if (!copied) return
    const id = setTimeout(() => setCopied(false), COPIED_RESET_MS)
    return () => clearTimeout(id)
  }, [copied])

  async function handleClick() {
    try {
      await navigator.clipboard.writeText(value)
      setCopied(true)
    } catch {
      // Clipboard access can be denied (permissions, an insecure
      // context) — not worth failing over for a convenience action; the
      // value is still shown on screen to copy by hand.
    }
  }

  return (
    <Button
      type="button"
      variant="outline"
      size="icon-sm"
      onClick={handleClick}
      aria-label={copied ? 'Copied' : label}
      className={cn(className)}
      {...props}
    >
      {copied ? <Check /> : <Copy />}
    </Button>
  )
}
