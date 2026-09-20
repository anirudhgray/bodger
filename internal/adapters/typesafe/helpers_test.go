package typesafe_test

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/anirudhgray/bodger/internal/domain"
	"github.com/anirudhgray/bodger/internal/domain/money"
)

// jsonResponse builds a fake *http.Response carrying body as its content,
// for use from a roundTripFunc fake transport — the same helper
// internal/adapters/fxprovider/helpers_test.go uses, so both adapters'
// tests read the same way.
func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// jsonResponseWithHeader is jsonResponse plus one response header, for
// tests exercising Retry-After.
func jsonResponseWithHeader(status int, body string, headerKey, headerValue string) *http.Response {
	resp := jsonResponse(status, body)
	resp.Header.Set(headerKey, headerValue)
	return resp
}

func mustMoney(amountMinor int64, currency string) money.Money {
	m, err := money.NewMoney(amountMinor, currency)
	if err != nil {
		panic(err)
	}
	return m
}

func mustDate(y int, m time.Month, d int) domain.Date {
	date, err := domain.NewDate(y, m, d)
	if err != nil {
		panic(err)
	}
	return date
}
