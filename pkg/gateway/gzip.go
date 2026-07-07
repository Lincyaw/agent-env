package gateway

import (
	"bufio"
	"compress/gzip"
	"net"
	"net/http"
	"strings"
	"sync"
)

var gzipWriterPool = sync.Pool{
	New: func() any { return gzip.NewWriter(nil) },
}

// gzipResponseWriter compresses the response body when the handler produces
// JSON or NDJSON. The decision is deferred to WriteHeader so streaming
// responses (SSE, file downloads) pass through untouched.
type gzipResponseWriter struct {
	http.ResponseWriter
	gz          *gzip.Writer
	wroteHeader bool
	compress    bool
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.wroteHeader = true
		ct := w.Header().Get("Content-Type")
		compressible := strings.HasPrefix(ct, "application/json") || strings.HasPrefix(ct, "application/x-ndjson")
		if compressible && w.Header().Get("Content-Encoding") == "" {
			w.compress = true
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Del("Content-Length")
			w.gz.Reset(w.ResponseWriter)
		}
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.compress {
		return w.gz.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

func (w *gzipResponseWriter) Flush() {
	if w.compress {
		_ = w.gz.Flush()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *gzipResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// GzipMiddleware compresses JSON responses for clients that accept gzip.
// Pool listings and trajectory payloads are highly repetitive, so this cuts
// transfer size by an order of magnitude on slow links.
func GzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		gz := gzipWriterPool.Get().(*gzip.Writer)
		gw := &gzipResponseWriter{ResponseWriter: w, gz: gz}
		defer func() {
			if gw.compress {
				_ = gw.gz.Close()
			}
			gzipWriterPool.Put(gz)
		}()
		next.ServeHTTP(gw, r)
	})
}
