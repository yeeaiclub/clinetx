// Copyright The yeeaiclub Authors
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientSendRetry(t *testing.T) {
	oldBackoff := httpRetryBackoff
	httpRetryBackoff = func(attempt int) time.Duration { return 5 * time.Millisecond }
	t.Cleanup(func() { httpRetryBackoff = oldBackoff })

	t.Run("retry off returns first error status", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		}))
		t.Cleanup(srv.Close)

		cli := NewClient()
		req := Request{BaseURL: srv.URL, Path: "v1", Method: http.MethodGet, AuthToken: "t"}
		resp, err := cli.Send(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
		assert.EqualValues(t, 1, calls.Load())
	})

	t.Run("retry on recovers after transient failures", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			n := calls.Add(1)
			if n < 3 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		t.Cleanup(srv.Close)

		cli := NewClient(WithRetry(true))
		req := Request{BaseURL: srv.URL, Path: "v1", Method: http.MethodGet, AuthToken: "t"}
		resp, err := cli.Send(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.JSONEq(t, `{"ok":true}`, string(resp.Body))
		assert.EqualValues(t, 3, calls.Load())
	})

	t.Run("retry on returns last response when all attempts exhausted", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		t.Cleanup(srv.Close)

		cli := NewClient(WithRetry(true))
		req := Request{BaseURL: srv.URL, Path: "v1", Method: http.MethodGet, AuthToken: "t"}
		resp, err := cli.Send(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
		assert.EqualValues(t, maxHTTPRetryAttempts, calls.Load())
	})

	t.Run("retry stops when context ends during backoff", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusTooManyRequests)
		}))
		t.Cleanup(srv.Close)

		httpRetryBackoff = func(int) time.Duration { return 200 * time.Millisecond }

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()

		cli := NewClient(WithRetry(true))
		req := Request{BaseURL: srv.URL, Path: "v1", Method: http.MethodGet, AuthToken: "t"}
		_, err := cli.Send(ctx, req)
		require.Error(t, err)
		assert.True(t, errors.Is(err, context.DeadlineExceeded))
		assert.EqualValues(t, 1, calls.Load())
	})
}

func TestClientSendTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`ok`))
	}))
	t.Cleanup(srv.Close)

	cli := NewClient(WithTimeout(50 * time.Millisecond))
	req := Request{BaseURL: srv.URL, Path: "v1", Method: http.MethodGet, AuthToken: "t"}
	_, err := cli.Send(context.Background(), req)
	require.Error(t, err)

	deadline := errors.Is(err, context.DeadlineExceeded)
	var netErr net.Error
	clientTimeout := errors.As(err, &netErr) && netErr.Timeout()
	slowStr := strings.Contains(err.Error(), "Client.Timeout")
	assert.True(t, deadline || clientTimeout || slowStr,
		"expected timeout-related error, got: %v", err)
}
