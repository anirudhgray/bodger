import { describe, expect, it } from 'vitest'

import { capitalize, sanitizeAmountInput } from './utils'

describe('capitalize', () => {
  it('uppercases only the first letter', () => {
    expect(capitalize('spend')).toBe('Spend')
  })

  it('leaves an empty string alone', () => {
    expect(capitalize('')).toBe('')
  })
})

describe('sanitizeAmountInput', () => {
  it('passes through a plain amount unchanged', () => {
    expect(sanitizeAmountInput('42.50')).toBe('42.50')
  })

  it('strips letters and other non-numeric characters', () => {
    expect(sanitizeAmountInput('soemthing')).toBe('')
    expect(sanitizeAmountInput('$42.50')).toBe('42.50')
    expect(sanitizeAmountInput('42.50 USD')).toBe('42.50')
  })

  it('keeps only the first decimal point', () => {
    expect(sanitizeAmountInput('4.2.5.0')).toBe('4.250')
  })

  it('allows an in-progress trailing decimal point', () => {
    expect(sanitizeAmountInput('42.')).toBe('42.')
  })
})
