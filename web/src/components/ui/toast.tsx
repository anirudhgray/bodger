// Hand-built against Radix's Toast primitive directly (not generated via
// `npx shadcn add toast` — shadcn's current registry only ships a
// sonner-based toast, and issue #108 deliberately keeps this app on one
// primitive library, Radix, rather than adding sonner alongside it for
// this one component). Wiring/variant conventions otherwise match the
// rest of components/ui (cva variants, `cn`, data-slot).
//
// State lives in ../../hooks/use-toast.ts's module-level store — Toaster
// is the only consumer of useToast(); everywhere else calls the plain
// toast() function.
import * as React from 'react'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from 'cn'
import { Toast as ToastPrimitive } from 'radix-ui'
import { CheckCircle2Icon, Loader2Icon, XCircleIcon, XIcon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import {
  dismissToast,
  useToast,
  type ToastItem,
  type ToastVariant,
} from '@/hooks/use-toast'

function ToastProvider({
  ...props
}: React.ComponentProps<typeof ToastPrimitive.Provider>) {
  return <ToastPrimitive.Provider data-slot="toast-provider" {...props} />
}

function ToastViewport({
  className,
  ...props
}: React.ComponentProps<typeof ToastPrimitive.Viewport>) {
  return (
    <ToastPrimitive.Viewport
      data-slot="toast-viewport"
      className={cn(
        'fixed right-0 bottom-0 z-100 flex w-full max-w-sm flex-col gap-2 p-4 outline-none sm:right-4 sm:bottom-4',
        className,
      )}
      {...props}
    />
  )
}

// Background/border stay neutral (`bg-popover`/`border-border`, the same
// surface every other overlay in this app uses) for every variant — per
// docs/design-system.md's "the accent is a signal, not a fill" principle,
// color lives on the leading icon (and, matching the existing "Recorded."
// convention, the title text) rather than washing the whole card, unlike
// the original bg-success/10-style full tint this replaces.
const toastVariants = cva(
  'group/toast pointer-events-auto relative flex w-full items-start gap-3 rounded-xl border border-border bg-popover p-4 text-sm text-popover-foreground shadow-lg ring-1 ring-foreground/10 data-[state=open]:animate-in data-[state=open]:slide-in-from-bottom-full data-[state=open]:sm:slide-in-from-right-full data-[state=closed]:animate-out data-[state=closed]:fade-out-80 data-[swipe=end]:animate-out data-[swipe=end]:fade-out-80 data-[swipe=move]:translate-x-[var(--radix-toast-swipe-move-x)] data-[swipe=cancel]:translate-x-0 data-[swipe=end]:translate-x-[var(--radix-toast-swipe-end-x)]',
)

const toastTitleVariants = cva('font-medium', {
  variants: {
    variant: {
      default: '',
      success: 'text-success',
      destructive: 'text-destructive',
      loading: 'text-muted-foreground',
    },
  },
  defaultVariants: {
    variant: 'default',
  },
})

const TOAST_ICONS: Record<ToastVariant, React.ReactNode> = {
  default: null,
  success: <CheckCircle2Icon className="mt-0.5 size-4 shrink-0 text-success" />,
  destructive: (
    <XCircleIcon className="mt-0.5 size-4 shrink-0 text-destructive" />
  ),
  loading: (
    <Loader2Icon className="mt-0.5 size-4 shrink-0 animate-spin text-muted-foreground" />
  ),
}

function Toast({
  className,
  ...props
}: React.ComponentProps<typeof ToastPrimitive.Root>) {
  return (
    <ToastPrimitive.Root
      data-slot="toast"
      className={cn(toastVariants(), className)}
      {...props}
    />
  )
}

function ToastTitle({
  className,
  variant,
  ...props
}: React.ComponentProps<typeof ToastPrimitive.Title> &
  VariantProps<typeof toastTitleVariants>) {
  return (
    <ToastPrimitive.Title
      data-slot="toast-title"
      className={cn(toastTitleVariants({ variant }), className)}
      {...props}
    />
  )
}

function ToastDescription({
  className,
  ...props
}: React.ComponentProps<typeof ToastPrimitive.Description>) {
  return (
    <ToastPrimitive.Description
      data-slot="toast-description"
      className={cn('text-sm', className)}
      {...props}
    />
  )
}

function ToastClose({
  className,
  ...props
}: React.ComponentProps<typeof ToastPrimitive.Close>) {
  return (
    <ToastPrimitive.Close data-slot="toast-close" asChild {...props}>
      <Button
        variant="ghost"
        size="icon-sm"
        className={cn('absolute top-2 right-2', className)}
      >
        <XIcon />
        <span className="sr-only">Dismiss</span>
      </Button>
    </ToastPrimitive.Close>
  )
}

function ToastBody({ title, description, variant }: ToastItem) {
  return (
    <div className="flex items-start gap-2">
      {TOAST_ICONS[variant]}
      <div className="grid flex-1 gap-1 pr-6">
        {title && <ToastTitle variant={variant}>{title}</ToastTitle>}
        {description && <ToastDescription>{description}</ToastDescription>}
      </div>
    </div>
  )
}

// Mounted once at the app root (main.tsx), alongside ThemeProvider.
function Toaster() {
  const { toasts } = useToast()

  return (
    <ToastProvider>
      {toasts.map((item) => (
        <Toast
          // Keyed on version, not just id: toast.promise updates a toast's
          // content/variant/duration in place (see use-toast.ts) rather
          // than creating a new one, and Radix's auto-dismiss timer only
          // starts counting from when its Root mounts — bumping the key
          // forces that remount so the post-update duration actually
          // takes effect instead of inheriting the original toast's timer.
          key={`${item.id}-${item.version}`}
          duration={item.duration}
          open={item.open}
          onOpenChange={(open) => {
            if (!open) dismissToast(item.id)
          }}
        >
          <ToastBody {...item} />
          <ToastClose />
        </Toast>
      ))}
      <ToastViewport />
    </ToastProvider>
  )
}

export {
  Toast,
  ToastClose,
  ToastDescription,
  ToastProvider,
  Toaster,
  ToastTitle,
  ToastViewport,
}
