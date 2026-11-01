package provider

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func slowAuthHeaderServer(t *testing.T, headerName, wantValue string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(headerName); got != wantValue {
			t.Errorf("%s = %q, want %q", headerName, got, wantValue)
		}
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
}
