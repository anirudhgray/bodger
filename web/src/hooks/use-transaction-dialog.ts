import { createContext, useContext, useEffect } from 'react'

import type { Transaction } from '@/lib/api'

export type SavedEvent =
  | { mode: 'create'; transaction: Transaction }
  | { mode: 'edit'; transaction: Transaction }
export type SavedListener = (event: SavedEvent) => void

export interface TransactionDialogContextValue {
  openCreate: (onSaved?: (created: Transaction) => void) => void
  openEdit: (
    transaction: Transaction,
    onSaved?: (updated: Transaction) => void,
  ) => void
  onTransactionSaved: (listener: SavedListener) => () => void
}

export const TransactionDialogContext =
  createContext<TransactionDialogContextValue | null>(null)

export function useTransactionDialog(): TransactionDialogContextValue {
  const context = useContext(TransactionDialogContext)
  if (context === null) {
    throw new Error(
      'useTransactionDialog must be used within a TransactionDialogProvider',
    )
  }
  return context
}

// useTransactionSaved subscribes to every create/edit, wherever it was
// triggered from — TransactionsList uses this to refresh itself rather
// than relying on being the one that opened the dialog in the first
// place (see SavedEvent's own comment for why that matters).
export function useTransactionSaved(listener: SavedListener) {
  const { onTransactionSaved } = useTransactionDialog()
  useEffect(() => onTransactionSaved(listener), [onTransactionSaved, listener])
}
