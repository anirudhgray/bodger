package money

import (
	"encoding/json"
	"fmt"
)

// moneyJSON is the wire shape for Money: the amount as a string (never a
// JSON number — a JSON number is a float in most parsers), alongside the
// currency code and the exponent that was used to render it, so a reader
// never has to guess at precision.
type moneyJSON struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
	Exponent int    `json:"exponent"`
}

// MarshalJSON implements json.Marshaler. See moneyJSON for the wire shape.
func (m Money) MarshalJSON() ([]byte, error) {
	c, ok := LookupCurrency(m.currency)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownCurrency, m.currency)
	}
	return json.Marshal(moneyJSON{
		Amount:   formatAmount(m.amountMinor, c.MinorUnitExponent),
		Currency: m.currency,
		Exponent: c.MinorUnitExponent,
	})
}

// UnmarshalJSON implements json.Unmarshaler. It always constructs the
// result through Parse/NewMoney, so a Money decoded off the wire is
// validated exactly the same way as one built in code — the wire-supplied
// "exponent" field is read only to catch a payload whose amount was
// rendered with a different exponent than this package's own currency
// data, never used to interpret the amount itself.
func (m *Money) UnmarshalJSON(data []byte) error {
	var wire moneyJSON
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	c, ok := LookupCurrency(wire.Currency)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownCurrency, wire.Currency)
	}
	if wire.Exponent != c.MinorUnitExponent {
		return fmt.Errorf("%w: %q has minor-unit exponent %d, but payload says %d", ErrInvalidAmountFormat, wire.Currency, c.MinorUnitExponent, wire.Exponent)
	}
	parsed, err := Parse(wire.Amount, wire.Currency)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}
