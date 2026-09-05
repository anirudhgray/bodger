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
import { XIcon } from 'lucide-react'

import { Button } from '@/components/ui/button'
import { dismissToast, useToast, type ToastItem } from '@/hooks/use-toast'

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

const toastVariants = cva(
  'group/toast pointer-events-auto relative flex w-full items-start gap-3 rounded-xl border bg-popover p-4 text-sm text-popover-foreground shadow-lg ring-1 ring-foreground/10 data-[state=open]:animate-in data-[state=open]:slide-in-from-bottom-full data-[state=open]:sm:slide-in-from-right-full data-[state=closed]:animate-out data-[state=closed]:fade-out-80 data-[swipe=end]:animate-out data-[swipe=end]:fade-out-80 data-[swipe=move]:translate-x-[var(--radix-toast-swipe-move-x)] data-[swipe=cancel]:translate-x-0 data-[swipe=end]:translate-x-[var(--radix-toast-swipe-end-x)]',
  {
    variants: {
      variant: {
        default: 'border-border',
        success: 'border-success/20 bg-success/10 text-success',
        destructive: 'border-destructive/20 bg-destructive/10 text-destructive',
      },
    },
    defaultVariants: {
      variant: 'default',
    },
  },
)

function Toast({
  className,
  variant,
  ...props
}: React.ComponentProps<typeof ToastPrimitive.Root> &
  VariantProps<typeof toastVariants>) {
  return (
    <ToastPrimitive.Root
      data-slot="toast"
      className={cn(toastVariants({ variant }), className)}
      {...props}
    />
  )
}

function ToastTitle({
  className,
  ...props
}: React.ComponentProps<typeof ToastPrimitive.Title>) {
  return (
    <ToastPrimitive.Title
      data-slot="toast-title"
      className={cn('font-medium', className)}
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

function ToastBody({ title, description }: ToastItem) {
  return (
    <div className="grid gap-1 pr-6">
      {title && <ToastTitle>{title}</ToastTitle>}
      {description && <ToastDescription>{description}</ToastDescription>}
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
          key={item.id}
          variant={item.variant}
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
