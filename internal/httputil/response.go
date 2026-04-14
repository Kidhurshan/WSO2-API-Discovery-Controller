package httputil

import (
	"fmt"
	"io"
	"net/http"
)

// MaxResponseBodySize is the default limit for response body reads (10 MB).
const MaxResponseBodySize int64 = 10 * 1024 * 1024

// ReadResponseBody reads up to maxBytes from an HTTP response body.
// If maxBytes <= 0, MaxResponseBodySize is used. Returns an error if the
// body exceeds the limit, preventing unbounded memory allocation from
// malicious or misconfigured external services.
func ReadResponseBody(resp *http.Response, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = MaxResponseBodySize
	}
	limited := io.LimitReader(resp.Body, maxBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("response body exceeds %d byte limit", maxBytes)
	}
	return body, nil
}
