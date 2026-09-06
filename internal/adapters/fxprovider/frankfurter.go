// Package fxprovider implements ports.FxRateProvider adapters for external
// FX rate services. Frankfurter (docs/decisions/0012-fx-rate-provider.md)
// is the only implementation today; the port stays provider-agnostic so a
// future replacement is a new adapter here, not a change above the port.
package fxprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/fx"
	"github.com/anirudhgray/bodger/internal/platform/errs"
	"github.com/anirudhgray/bodger/internal/ports"
)

// DefaultBaseURL is Frankfurter's public hosted instance (ADR-0012's
// fx.provider_base_url default). A self-hoster running their own instance
// passes a different baseURL to New instead.
const DefaultBaseURL = "https://api.frankfurter.dev"

// DefaultTimeout sits comfortably above Frankfurter's public-instance
// ~5s P95 latency (ADR-0012) without leaving a hung request open
// indefinitely on a routine outage.
const DefaultTimeout = 15 * time.Second

// Frankfurter is a ports.FxRateProvider backed by the Frankfurter v2 API
// (https://api.frankfurter.dev/v2). It holds no mutable state and is safe
// for concurrent use, though ADR-0012 asks callers to serialize their own
// requests rather than fan out over pairs or dates.
type Frankfurter struct {
	baseURL string
	client  *http.Client
}

// New constructs a Frankfurter adapter. baseURL is normally
// DefaultBaseURL; an empty string is treated as DefaultBaseURL, and any
// other value points the adapter at a self-hosted instance (ADR-0012's
// fx.provider_base_url). client is normally nil, which constructs an
// *http.Client with DefaultTimeout; a caller with its own timeout or
// transport requirements -- including a test pointing at a fake
// transport or an httptest.Server -- can pass any *http.Client.
func New(baseURL string, client *http.Client) *Frankfurter {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: DefaultTimeout}
	}
	return &Frankfurter{baseURL: strings.TrimRight(baseURL, "/"), client: client}
}

var _ ports.FxRateProvider = (*Frankfurter)(nil)

// rateRow is one row of a Frankfurter response. Rate is a json.Number, not
// a float64: ADR-0012 requires decoding the wire's JSON number as text and
// building the decimal from that text, never round-tripping through
// float64 -- see frankfurter_precision_test.go.
type rateRow struct {
	Date  string      `json:"date"`
	Base  string      `json:"base"`
	Quote string      `json:"quote"`
	Rate  json.Number `json:"rate"`
}

// toProviderRate converts a decoded rateRow into a ports.ProviderRate,
// rejecting a row dated later than upperBound -- ADR-0012's "reject a
// returned date later than the requested one" -- and building the rate's
// decimal value from the JSON number's string form rather than through
// float64.
func (r rateRow) toProviderRate(upperBound domain.Date) (ports.ProviderRate, error) {
	rowDate, err := parseDate(r.Date)
	if err != nil {
		return ports.ProviderRate{}, errs.New(errs.Internal).
			Explain("the rate provider returned an unparseable date %q", r.Date).
			Wrap(err)
	}
	if rowDate.After(upperBound) {
		return ports.ProviderRate{}, errs.New(errs.Internal).
			Explain("the rate provider returned a rate dated %s, after the requested date %s", rowDate, upperBound).
			Wrap(fmt.Errorf("provider row date %s is after requested date %s", rowDate, upperBound))
	}
	value, err := decimal.NewFromString(r.Rate.String())
	if err != nil {
		return ports.ProviderRate{}, errs.New(errs.Internal).
			Explain("the rate provider returned a rate that isn't a decimal number: %q", r.Rate.String()).
			Wrap(err)
	}
	rate, err := fx.NewRate(r.Base, r.Quote, value)
	if err != nil {
		return ports.ProviderRate{}, errs.New(errs.Internal).
			Explain("the rate provider returned a rate bodger couldn't accept").
			Wrap(err)
	}
	return ports.ProviderRate{Rate: rate, Date: rowDate}, nil
}

func parseDate(s string) (domain.Date, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return domain.Date{}, err
	}
	return domain.NewDate(t.Year(), t.Month(), t.Day())
}

// FetchRate implements ports.FxRateProvider.
func (f *Frankfurter) FetchRate(ctx context.Context, base, quote string, date domain.Date) (ports.ProviderRate, error) {
	reqURL := fmt.Sprintf("%s/v2/rate/%s/%s?date=%s",
		f.baseURL, url.PathEscape(base), url.PathEscape(quote), date.String())

	body, err := f.get(ctx, reqURL)
	if err != nil {
		return ports.ProviderRate{}, err
	}
	defer func() { _ = body.Close() }()

	dec := json.NewDecoder(body)
	dec.UseNumber()
	var row rateRow
	if err := dec.Decode(&row); err != nil {
		return ports.ProviderRate{}, errs.New(errs.Internal).
			Explain("the rate provider returned a response bodger couldn't parse").
			Wrap(err)
	}
	return row.toProviderRate(date)
}

// FetchRange implements ports.FxRateProvider, using Frankfurter's
// from/to time-series primitive (ADR-0012) rather than looping over
// individual dates.
func (f *Frankfurter) FetchRange(ctx context.Context, base, quote string, from, to domain.Date) ([]ports.ProviderRate, error) {
	reqURL := fmt.Sprintf("%s/v2/rates?base=%s&quotes=%s&from=%s&to=%s",
		f.baseURL, url.QueryEscape(base), url.QueryEscape(quote), from.String(), to.String())

	body, err := f.get(ctx, reqURL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()

	dec := json.NewDecoder(body)
	dec.UseNumber()
	var rows []rateRow
	if err := dec.Decode(&rows); err != nil {
		return nil, errs.New(errs.Internal).
			Explain("the rate provider returned a response bodger couldn't parse").
			Wrap(err)
	}
	if len(rows) == 0 {
		return nil, errs.New(errs.NotFound).
			Explain("no rate data for %s/%s between %s and %s", base, quote, from, to)
	}

	out := make([]ports.ProviderRate, 0, len(rows))
	for _, row := range rows {
		pr, err := row.toProviderRate(to)
		if err != nil {
			return nil, err
		}
		out = append(out, pr)
	}
	return out, nil
}

// get performs a GET request against the provider and maps its outcome
// per ADR-0012's status table: a transport-level failure (unreachable
// host, DNS failure, connection refused, timeout) and a 5xx response both
// mean "provider unreachable" (errs.Unavailable); 404 means "no data for
// this pair/date" (errs.NotFound); 422 means the request itself was
// rejected -- an unknown currency or bad parameter (errs.InvalidInput). A
// 200 response's body is returned unread for the caller to decode.
func (f *Frankfurter) get(ctx context.Context, reqURL string) (io.ReadCloser, error) {
	req, buildErr := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if buildErr != nil {
		return nil, errs.New(errs.Internal).
			Explain("could not build a request to the rate provider").
			Wrap(buildErr)
	}

	resp, doErr := f.client.Do(req)
	if doErr != nil {
		return nil, errs.New(errs.Unavailable).
			Explain("could not reach the rate provider").
			Wrap(doErr)
	}

	switch {
	case resp.StatusCode == http.StatusOK:
		return resp.Body, nil
	case resp.StatusCode == http.StatusNotFound:
		defer func() { _ = resp.Body.Close() }()
		return nil, errs.New(errs.NotFound).
			Explain("the rate provider has no data for this pair/date")
	case resp.StatusCode == http.StatusUnprocessableEntity:
		defer func() { _ = resp.Body.Close() }()
		return nil, errs.New(errs.InvalidInput).
			Explain("the rate provider rejected the currency code or request parameters")
	case resp.StatusCode >= 500:
		defer func() { _ = resp.Body.Close() }()
		return nil, errs.New(errs.Unavailable).
			Explain("the rate provider is unavailable (status %d)", resp.StatusCode)
	default:
		defer func() { _ = resp.Body.Close() }()
		return nil, errs.New(errs.Internal).
			Explain("the rate provider returned an unexpected status %d", resp.StatusCode)
	}
}
