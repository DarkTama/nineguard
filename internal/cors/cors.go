package cors

import (
	"net/http"
	"strings"
)

// Middleware enforces opt-in CORS on /v1, /v1/*, and /v1beta/* proxy paths.
// When allowedOrigins is empty, CORS is disabled and requests pass through unchanged.
func Middleware(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if o != "" {
			allowed[o] = struct{}{}
		}
	}

	return func(next http.Handler) http.Handler {
		if len(allowed) == 0 {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isProxyPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			origin := r.Header.Get("Origin")
			_, originAllowed := allowed[origin]

			if originAllowed && r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				stripAccessControlHeaders(w.Header())
				w.Header().Set("Access-Control-Allow-Origin", origin)
				addVaryOrigin(w.Header())
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				w.Header().Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}

			rw := &responseWriter{ResponseWriter: w}
			if originAllowed {
				rw.allowedOrigin = origin
			}
			next.ServeHTTP(rw, r)
			if !rw.wroteHeader {
				rw.applyHeaders()
			}
		})
	}
}

func isProxyPath(path string) bool {
	return path == "/v1" || strings.HasPrefix(path, "/v1/") || strings.HasPrefix(path, "/v1beta/")
}

func stripAccessControlHeaders(h http.Header) {
	for k := range h {
		if strings.HasPrefix(strings.ToLower(k), "access-control-") {
			delete(h, k)
		}
	}
}

func addVaryOrigin(h http.Header) {
	for _, v := range h.Values("Vary") {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), "Origin") {
				return
			}
		}
	}
	h.Add("Vary", "Origin")
}

type responseWriter struct {
	http.ResponseWriter
	allowedOrigin string
	wroteHeader   bool
}

func (rw *responseWriter) applyHeaders() {
	h := rw.Header()
	stripAccessControlHeaders(h)
	if rw.allowedOrigin != "" {
		h.Set("Access-Control-Allow-Origin", rw.allowedOrigin)
		addVaryOrigin(h)
	}
}

func (rw *responseWriter) WriteHeader(statusCode int) {
	if rw.wroteHeader {
		return
	}
	rw.wroteHeader = true
	rw.applyHeaders()
	rw.ResponseWriter.WriteHeader(statusCode)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

func (rw *responseWriter) Flush() {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}
