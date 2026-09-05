// A searchable, single-select popover list (Radix Popover + cmdk's
// Command, shadcn's Combobox pattern) — issue #106's replacement for a
// native <select> wherever a picker needs to show real indentation or
// host an inline "create new" row, neither of which a plain <option>
// list or a grouped Radix Select (one level of nesting only) can do.
// Composes components/ui/popover.tsx and components/ui/command.tsx the
// same way components/ui/date-picker.tsx composes popover.tsx and
// calendar.tsx into one higher-level widget.
//
// Generic over ComboboxOption rather than category-specific: its first
// and only caller today is the category tree
// (lib/category-tree.ts's CategoryTreeNode → ComboboxOption), but
// nothing here actually knows what a category is.
import * as React from 'react'
import { CheckIcon, ChevronsUpDownIcon, PlusIcon } from 'lucide-react'
import { cn } from 'cn'

import { Button } from '@/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'

export interface ComboboxOption {
  value: string
  // Shown for this option in the open list, indented by `depth`.
  label: string
  // Indentation level for a hierarchical list (0 = top level, 1 = its
  // direct children, ...) — see lib/category-tree.ts's
  // CategoryTreeNode.depth, this component's only source of it so far.
  depth?: number
  // Full "Parent > Child" ancestry chain, shown in the trigger once
  // collapsed (where indentation alone would be invisible) and matched
  // against as search text, so typing either a category's own name or
  // an ancestor's still finds it. Falls back to `label` when absent —
  // a flat, non-hierarchical option list has nothing else to show here.
  path?: string
}

interface ComboboxBaseProps {
  options: ComboboxOption[]
  placeholder?: string
  searchPlaceholder?: string
  emptyText?: string
  id?: string
  'aria-label'?: string
  className?: string
  disabled?: boolean
  // Offers to create whatever text is currently typed once it matches no
  // existing option's label, as an inline row in the list itself. Should
  // resolve to the option the caller wants selected next (e.g.
  // TransactionDialog's createCategory response) — the combobox commits
  // that value and closes, the same as picking an existing option.
  // Rejecting leaves the popover open so the caller's own error handling
  // (a toast, an inline message) can run without losing the transaction
  // mid-entry.
  onCreate?: (inputValue: string) => Promise<ComboboxOption>
  createLabel?: (inputValue: string) => string
}

type ComboboxProps = ComboboxBaseProps & {
  // Controlled mode (TransactionDialog's category field, alongside its
  // other useState-backed fields): pass value+onChange, omit name.
  value?: string
  onChange?: (value: string) => void
  // Uncontrolled mode, for TransactionsList's plain <form>+FormData
  // filter panel (see components/ui/date-picker.tsx's identical
  // convention, for the same reason): pass name (+ optional
  // defaultValue), omit value/onChange. A hidden input mirrors the
  // picked value so FormData(form) reads it the same way it would a
  // native <select>'s value.
  name?: string
  defaultValue?: string
}

function Combobox({
  options,
  value,
  onChange,
  name,
  defaultValue,
  placeholder = 'Select…',
  searchPlaceholder = 'Search…',
  emptyText = 'No results.',
  id,
  className,
  disabled,
  onCreate,
  createLabel = (inputValue) => `Create "${inputValue}"`,
  ...rest
}: ComboboxProps) {
  const [open, setOpen] = React.useState(false)
  const [search, setSearch] = React.useState('')
  const [creating, setCreating] = React.useState(false)
  const [internalValue, setInternalValue] = React.useState(defaultValue ?? '')
  const isControlled = value !== undefined
  const currentValue = isControlled ? value : internalValue

  const selected = options.find((option) => option.value === currentValue)

  function commit(next: string) {
    if (isControlled) {
      onChange?.(next)
    } else {
      setInternalValue(next)
    }
  }

  function handleSelect(option: ComboboxOption) {
    commit(option.value)
    setOpen(false)
    setSearch('')
  }

  const trimmedSearch = search.trim()
  const hasExactMatch = options.some(
    (option) => option.label.toLowerCase() === trimmedSearch.toLowerCase(),
  )
  const showCreate = Boolean(onCreate) && trimmedSearch !== '' && !hasExactMatch

  async function handleCreate() {
    if (!onCreate || trimmedSearch === '' || creating) return
    setCreating(true)
    try {
      const created = await onCreate(trimmedSearch)
      commit(created.value)
      setOpen(false)
      setSearch('')
    } finally {
      setCreating(false)
    }
  }

  return (
    <>
      {name && <input type="hidden" name={name} value={currentValue} />}
      <Popover
        open={open}
        onOpenChange={(next) => {
          setOpen(next)
          if (!next) setSearch('')
        }}
      >
        <PopoverTrigger asChild>
          <Button
            id={id}
            type="button"
            variant="outline"
            role="combobox"
            aria-expanded={open}
            aria-label={rest['aria-label']}
            disabled={disabled}
            className={cn(
              'w-full min-w-0 justify-between font-normal',
              !selected && 'text-muted-foreground',
              className,
            )}
          >
            <span className="truncate">
              {selected ? (selected.path ?? selected.label) : placeholder}
            </span>
            <ChevronsUpDownIcon className="shrink-0 opacity-50" />
          </Button>
        </PopoverTrigger>
        <PopoverContent
          align="start"
          className="w-(--radix-popper-anchor-width) min-w-56 p-0"
        >
          <Command value={currentValue} label={searchPlaceholder}>
            <CommandInput
              placeholder={searchPlaceholder}
              value={search}
              onValueChange={setSearch}
            />
            <CommandList>
              <CommandEmpty>{emptyText}</CommandEmpty>
              <CommandGroup>
                {options.map((option) => (
                  <CommandItem
                    key={option.value}
                    value={option.value}
                    keywords={[option.path ?? option.label]}
                    onSelect={() => handleSelect(option)}
                  >
                    <CheckIcon
                      className={cn(
                        option.value === currentValue
                          ? 'opacity-100'
                          : 'opacity-0',
                      )}
                    />
                    <span
                      className="truncate"
                      style={{
                        paddingLeft: `${(option.depth ?? 0) * 0.75}rem`,
                      }}
                    >
                      {option.label}
                    </span>
                  </CommandItem>
                ))}
              </CommandGroup>
              {showCreate && (
                <CommandGroup>
                  <CommandItem
                    value={`__create__:${trimmedSearch}`}
                    disabled={creating}
                    onSelect={handleCreate}
                  >
                    <PlusIcon />
                    {creating ? 'Creating…' : createLabel(trimmedSearch)}
                  </CommandItem>
                </CommandGroup>
              )}
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>
    </>
  )
}

export { Combobox }
