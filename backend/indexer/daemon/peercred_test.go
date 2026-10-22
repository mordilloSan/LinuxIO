package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/peercred"
)

func TestAuthorizeTransportMiddlewareRequiresRootUnix(t *testing.T) {
	withUnixConnection := func(ctx context.Context) context.Context {
		return context.WithValue(ctx, connectionKindContextKey{}, true)
	}
	for _, test := range []struct {
		name    string
		context context.Context
		want    int
	}{
		{name: "root Unix", context: peercred.WithCredForTest(withUnixConnection(context.Background()), 0), want: http.StatusNoContent},
		{name: "non-root Unix", context: peercred.WithCredForTest(withUnixConnection(context.Background()), 1000), want: http.StatusForbidden},
		{name: "missing transport", context: context.Background(), want: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := authorizeTransportMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(http.MethodGet, "/status", nil).WithContext(test.context)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != test.want {
				t.Fatalf("status = %d, want %d", rr.Code, test.want)
			}
		})
	}
}
