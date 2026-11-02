package provider

import (
	"context"
	"crynux_as/config"
	"io"
	"net"
	"net/http"
	"time"
)

func providerDialer(cfg config.ProviderServiceConfig) *net.Dialer {
	return &net.Dialer{
		Timeout:   time.Duration(cfg.ConnectTimeoutSeconds) * time.Second,
		KeepAlive: 30 * time.Second,
	}
}

func newProviderHTTPClient(cfg config.ProviderServiceConfig) *http.Client {
	connectTimeout := time.Duration(cfg.ConnectTimeoutSeconds) * time.Second
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           providerDialer(cfg).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   connectTimeout,
			ResponseHeaderTimeout: time.Duration(cfg.ResponseHeaderTimeoutSeconds) * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

func withNonStreamRequestTimeout(ctx context.Context, stream bool, requestTimeoutSeconds uint64) (context.Context, context.CancelFunc) {
	if stream {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, time.Duration(requestTimeoutSeconds)*time.Second)
}

// withStreamIdleTimeout closes body when no bytes are read for idleSeconds.
// Non-closer readers (unit-test fixtures) are returned unchanged.
func withStreamIdleTimeout(body io.Reader, idleSeconds uint64) (io.Reader, func()) {
	return withStreamIdleTimeoutDuration(body, time.Duration(idleSeconds)*time.Second)
}

func withStreamIdleTimeoutDuration(body io.Reader, idle time.Duration) (io.Reader, func()) {
	if idle <= 0 {
		return body, func() {}
	}
	rc, ok := body.(io.ReadCloser)
	if !ok {
		return body, func() {}
	}
	timer := time.AfterFunc(idle, func() {
		_ = rc.Close()
	})
	return &streamIdleReader{r: body, timer: timer, idle: idle}, func() { timer.Stop() }
}

type streamIdleReader struct {
	r     io.Reader
	timer *time.Timer
	idle  time.Duration
}

func (r *streamIdleReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if n > 0 {
		r.timer.Reset(r.idle)
	}
	return n, err
}
