package middleware

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/requesttiming"
	"github.com/gin-gonic/gin"
)

// RequestTimingResponseWriter measures the time spent writing bytes from the
// gateway to the client. For streaming responses this is the sum of write
// durations and excludes time waiting for the next upstream token.
type RequestTimingResponseWriter struct {
	gin.ResponseWriter
	ctx context.Context
}

func NewRequestTimingResponseWriter(writer gin.ResponseWriter, ctx context.Context) *RequestTimingResponseWriter {
	return &RequestTimingResponseWriter{ResponseWriter: writer, ctx: ctx}
}

func (w *RequestTimingResponseWriter) Write(data []byte) (int, error) {
	startedAt := time.Now()
	n, err := w.ResponseWriter.Write(data)
	requesttiming.AddDuration(w.ctx, "client_response_write", time.Since(startedAt))
	return n, err
}

func (w *RequestTimingResponseWriter) WriteString(data string) (int, error) {
	startedAt := time.Now()
	n, err := w.ResponseWriter.WriteString(data)
	requesttiming.AddDuration(w.ctx, "client_response_write", time.Since(startedAt))
	return n, err
}

func (w *RequestTimingResponseWriter) Flush() {
	startedAt := time.Now()
	w.ResponseWriter.Flush()
	requesttiming.AddDuration(w.ctx, "client_response_write", time.Since(startedAt))
}
