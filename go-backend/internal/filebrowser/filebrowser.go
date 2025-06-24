package filebrowser

import (
	"context"
	"net/http"
	"net/http/httputil"
	"net/url"

	"go-backend/internal/session"

	"github.com/gin-gonic/gin"
)

type ctxKey string

const proxyPathKey ctxKey = "proxyPath"

func FilebrowserReverseProxy() gin.HandlerFunc {
	target, _ := url.Parse("http://127.0.0.1:8090")
	proxy := httputil.NewSingleHostReverseProxy(target)

	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host

		// Extract session_id cookie manually
		cookie, err := req.Cookie("session_id")
		if err == nil && cookie.Value != "" {
			sess := session.Get(cookie.Value)
			if sess != nil {
				req.Header.Set("proxy-user", sess.User.Name)
			}
		}
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Set("X-Frame-Options", "SAMEORIGIN")
		return nil
	}

	return func(c *gin.Context) {
		proxyPath := c.Param("proxyPath")
		c.Request = c.Request.WithContext(
			context.WithValue(c.Request.Context(), proxyPathKey, proxyPath),
		)

		defer func() {
			if rec := recover(); rec != nil {
				if err, ok := rec.(error); ok && err == http.ErrAbortHandler {
					// client closed connection — safe to ignore
					return
				}
				if str, ok := rec.(string); ok && str == "net/http: abort Handler" {
					return
				}
				// unexpected panic, rethrow
				panic(rec)
			}
		}()

		proxy.ServeHTTP(c.Writer, c.Request)
	}

}
