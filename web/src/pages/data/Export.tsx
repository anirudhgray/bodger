// The Export subpage of the Import & export area (issue #214), wrapping
// #213's REST surface: a full canonical JSON backup (ADR-0008), or a
// filtered CSV. Both routes hand back the file's raw bytes rather than a
// JSON envelope (lib/api.ts's downloadFile/saveBlob) — this screen only
// triggers the download and reports a failure; the file's own content is
// entirely server-computed (docs/architecture.md §3).
import { useEffect, useMemo, useState, type FormEvent } from 'react'
import { DownloadIcon } from 'lucide-react'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Combobox, type ComboboxOption } from '@/components/ui/combobox'
import { DatePicker } from '@/components/ui/date-picker'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  downloadExportCSV,
  downloadExportJSON,
  listAccounts,
  listCategories,
  type Account,
  type Category,
  type TransactionKind,
} from '@/lib/api'
import { buildCategoryTree } from '@/lib/category-tree'
import { errorMessage } from './shared'

// Same "Any" sentinel TransactionsList.tsx's own filter form uses —
// Radix's Select rejects an empty-string item value outright.
const ANY_FILTER_VALUE = 'any'

const KIND_OPTIONS: { value: TransactionKind; label: string }[] = [
  { value: 'outflow', label: 'Outflow' },
  { value: 'inflow', label: 'Inflow' },
  { value: 'transfer', label: 'Transfer' },
]

export function ExportPage() {
  const [accounts, setAccounts] = useState<Account[]>([])
  const [categories, setCategories] = useState<Category[]>([])
  const [jsonBusy, setJsonBusy] = useState(false)
  const [csvBusy, setCsvBusy] = useState(false)

  useEffect(() => {
    listAccounts()
      .then(setAccounts)
      .catch((err: unknown) => toast.error(errorMessage(err)))
    listCategories()
      .then(setCategories)
      .catch((err: unknown) => toast.error(errorMessage(err)))
  }, [])

  const categoryOptions = useMemo<ComboboxOption[]>(
    () => [
      { value: '', label: 'Any' },
      ...buildCategoryTree(categories).map(({ category, depth, path }) => ({
        value: category.id,
        label: category.name,
        depth,
        path,
      })),
    ],
    [categories],
  )

  async function handleJSON() {
    setJsonBusy(true)
    try {
      await downloadExportJSON()
      toast.success('Backup downloaded.')
    } catch (err) {
      toast.error(errorMessage(err))
    } finally {
      setJsonBusy(false)
    }
  }

  async function handleCSV(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setCsvBusy(true)
    try {
      const form = new FormData(event.currentTarget)
      const account = form.get('account') as string
      const type = form.get('type') as string
      await downloadExportCSV({
        account: account && account !== ANY_FILTER_VALUE ? account : undefined,
        category: (form.get('category') as string) || undefined,
        type:
          type && type !== ANY_FILTER_VALUE
            ? (type as TransactionKind)
            : undefined,
        from: (form.get('from') as string) || undefined,
        to: (form.get('to') as string) || undefined,
      })
      toast.success('Export downloaded.')
    } catch (err) {
      toast.error(errorMessage(err))
    } finally {
      setCsvBusy(false)
    }
  }

  return (
    <section className="flex flex-col gap-6">
      <p className="text-muted-foreground text-sm">
        Download your data — a complete backup you can restore from later, or a
        spreadsheet-friendly CSV of your transactions.
      </p>

      <Card className="max-w-lg">
        <CardHeader>
          <CardTitle>Full backup</CardTitle>
          <CardDescription>
            Every account, category, and transaction you have, as one JSON file.
            This is what the restore flow reads back in.
          </CardDescription>
        </CardHeader>
        <CardFooter>
          <Button onClick={handleJSON} disabled={jsonBusy}>
            <DownloadIcon />
            {jsonBusy ? 'Preparing…' : 'Download backup (JSON)'}
          </Button>
        </CardFooter>
      </Card>

      <Card className="max-w-2xl gap-4">
        <CardHeader>
          <CardTitle>Transactions as CSV</CardTitle>
          <CardDescription>
            One row per posting, for spreadsheets. A split transaction can’t be
            represented unambiguously in this format, so this isn’t something
            you can re-import — use the backup above for that.
          </CardDescription>
        </CardHeader>
        <form onSubmit={handleCSV}>
          <CardContent>
            <div className="flex flex-wrap items-end gap-3 sm:gap-4">
              <div className="flex flex-col gap-1">
                <Label htmlFor="export-account">Account</Label>
                <Select name="account" defaultValue={ANY_FILTER_VALUE}>
                  <SelectTrigger id="export-account">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={ANY_FILTER_VALUE}>Any</SelectItem>
                    {accounts.map((a) => (
                      <SelectItem key={a.id} value={a.id}>
                        {a.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="flex flex-col gap-1">
                <Label htmlFor="export-category">Category</Label>
                <Combobox
                  id="export-category"
                  name="category"
                  defaultValue=""
                  options={categoryOptions}
                  placeholder="Any"
                  searchPlaceholder="Search categories…"
                  emptyText="No matching categories."
                  className="w-44"
                />
              </div>
              <div className="flex flex-col gap-1">
                <Label htmlFor="export-type">Type</Label>
                <Select name="type" defaultValue={ANY_FILTER_VALUE}>
                  <SelectTrigger id="export-type">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={ANY_FILTER_VALUE}>Any</SelectItem>
                    {KIND_OPTIONS.map((k) => (
                      <SelectItem key={k.value} value={k.value}>
                        {k.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="flex flex-col gap-1">
                <Label htmlFor="export-from">From</Label>
                <DatePicker id="export-from" name="from" className="w-36" />
              </div>
              <div className="flex flex-col gap-1">
                <Label htmlFor="export-to">To</Label>
                <DatePicker id="export-to" name="to" className="w-36" />
              </div>
            </div>
          </CardContent>
          <CardFooter className="mt-4">
            <Button type="submit" disabled={csvBusy}>
              <DownloadIcon />
              {csvBusy ? 'Preparing…' : 'Download CSV'}
            </Button>
          </CardFooter>
        </form>
      </Card>
    </section>
  )
}
