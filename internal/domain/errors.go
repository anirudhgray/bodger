package domain

import "errors"

// ErrInvalidDate is returned by NewDate when the given year/month/day is
// not a real calendar date (an out-of-range month, a day that doesn't
// exist in that month, day 0, and so on).
var ErrInvalidDate = errors.New("domain: invalid calendar date")
