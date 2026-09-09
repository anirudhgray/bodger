// The Currency subpage of Settings (issue #140): the one field #132/#137
// add on top of the existing four sections (password/tokens/accounts/
// categories, issue #107) — the reporting currency balances and reports
// convert into (docs/decisions/0004-multi-currency-and-fx.md). Follows the
// same "one screen, exactly one existing application call per action"
// discipline as Password.tsx.
import { useEffect, useState, type FormEvent } from 'react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { getReportingCurrency, setReportingCurrency } from '@/lib/settings'
import { errorMessage } from './shared'

export function CurrencySettings() {
  const [currency, setCurrency] = useState('')
  const [isSet, setIsSet] = useState(false)
  const [effectiveCurrency, setEffectiveCurrency] = useState('')
  const [loading, setLoading] = useState(true)
  // The initial getReportingCurrency() fetch is a load failure, so it
  // stays inline (docs/design-system.md's "Toasts vs. inline messages")
  // — distinct from the save action's own failure below, which reports
  // via a toast instead.
  const [loadError, setLoadError] = useState<string | null>(null)
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    let cancelled = false
    getReportingCurrency()
      .then((result) => {
        if (cancelled) return
        setCurrency(result.currency)
        setIsSet(result.is_set)
        setEffectiveCurrency(result.effectiveCurrency)
      })
      .catch((err) => {
        if (!cancelled) setLoadError(errorMessage(err))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const value = currency.trim().toUpperCase()
    setSubmitting(true)
    // Setting the reporting currency is a user-triggered action, so both
    // outcomes report via toast now (docs/design-system.md's "Toasts vs.
    // inline messages") — `error` is a real option here, not omitted.
    const saving = (async () => {
      await setReportingCurrency(value)
      setCurrency(value)
      setIsSet(true)
    })()
    toast.promise(saving, {
      loading: 'Saving…',
      success: 'Reporting currency saved.',
      error: (err) => errorMessage(err),
    })
    try {
      await saving
    } catch {
      // Failure is already reported via the toast.promise `error` option
      // above.
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <section className="flex flex-col gap-4">
      <p className="text-muted-foreground text-sm">
        Balances and reports convert into this currency when you ask them to
        convert.
      </p>
      {loading ? (
        <p className="text-muted-foreground text-sm">Loading…</p>
      ) : (
        <form
          className="flex max-w-xs flex-col gap-4"
          onSubmit={handleSubmit}
          noValidate
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="reporting-currency">Reporting currency</Label>
            <Input
              id="reporting-currency"
              value={currency}
              onChange={(event) =>
                setCurrency(event.target.value.toUpperCase())
              }
              placeholder="e.g. USD"
              maxLength={3}
              className="uppercase"
              required
            />
            {!isSet && (
              <p className="text-muted-foreground text-xs">
                Not set — falls back to this instance's default currency (
                {effectiveCurrency}).
              </p>
            )}
          </div>
          {loadError && (
            <p role="alert" className="text-destructive text-sm">
              {loadError}
            </p>
          )}
          <Button
            type="submit"
            disabled={submitting || currency.trim() === ''}
            className="self-start"
          >
            {submitting ? 'Saving…' : 'Save'}
          </Button>
        </form>
      )}
    </section>
  )
}
