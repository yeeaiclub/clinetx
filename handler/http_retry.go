// Copyright The yeeaiclub Authors
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"
)

const maxHTTPRetryAttempts = 3

func defaultHTTPRetryBackoff(attempt int) time.Duration {
	return time.Duration(attempt+1) * time.Second
}

// httpRetryBackoff is wait duration before the next attempt after a retryable response (attempt is 0-based).
// Tests may replace it with shorter delays.
var httpRetryBackoff = defaultHTTPRetryBackoff

func shouldRetryHTTPStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

func sleepWithCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func doWithRetry(client *http.Client, req *http.Request) (*http.Response, error) {
	// Retry must be able to re-send the request body.
	// net/http consumes req.Body on the first Do, so we cache it here
	// and create a fresh Body for each retry attempt.
	var (
		bodyBytes     []byte
		hasBodyToSend = req.Body != nil
	)
	if hasBodyToSend {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		_ = req.Body.Close()
	}

	var resp *http.Response
	var err error
	for i := range maxHTTPRetryAttempts {
		if resp != nil {
			resp.Body.Close()
		}

		attemptReq := req.Clone(req.Context())
		if hasBodyToSend {
			attemptReq.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			attemptReq.ContentLength = int64(len(bodyBytes))
		}

		resp, err = client.Do(attemptReq)
		if err != nil {
			return nil, err
		}
		if !shouldRetryHTTPStatus(resp.StatusCode) {
			return resp, nil
		}

		if i < maxHTTPRetryAttempts-1 {
			if err = sleepWithCtx(req.Context(), httpRetryBackoff(i)); err != nil {
				resp.Body.Close()
				return nil, err
			}
		}
	}
	return resp, nil
}
