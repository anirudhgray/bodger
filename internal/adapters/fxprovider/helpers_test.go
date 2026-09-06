package fxprovider_test

import (
	"io"
	"net/http"
	"strings"
)

// jsonResponse builds a fake *http.Response carrying body as its content,
// for use from a roundTripFunc fake transport.
func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}
