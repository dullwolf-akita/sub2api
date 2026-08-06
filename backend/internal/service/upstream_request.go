package service

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/requesttiming"
	"github.com/klauspost/compress/zstd"
)

const upstreamRequestCompressionThreshold = 4 << 20

// PrepareUpstreamRequest applies the account's explicitly trusted request
// compression policy to the final JSON body. It is intentionally opt-in:
// arbitrary OpenAI-compatible endpoints must not receive zstd by default.
func PrepareUpstreamRequest(req *http.Request, body []byte, account *Account) (*http.Request, []byte, error) {
	if req == nil {
		return req, body, nil
	}
	requesttiming.SetBytes(req.Context(), "upstream_body_bytes", len(body))
	requesttiming.SetBytes(req.Context(), "upstream_wire_bytes", len(body))
	if len(body) < upstreamRequestCompressionThreshold || account == nil || !account.IsUpstreamRequestZstdEnabled() {
		return req, body, nil
	}

	startedAt := time.Now()
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, nil, err
	}
	compressed := encoder.EncodeAll(body, nil)
	encoder.Close()

	if len(compressed) >= len(body) {
		return req, body, nil
	}

	req.Body = http.NoBody
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(compressed)), nil
	}
	req.Body, _ = req.GetBody()
	req.ContentLength = int64(len(compressed))
	req.Header.Set("Content-Length", strconv.Itoa(len(compressed)))
	req.Header.Set("Content-Encoding", "zstd")
	requesttiming.SetBytes(req.Context(), "upstream_body_bytes", len(body))
	requesttiming.SetBytes(req.Context(), "upstream_wire_bytes", len(compressed))
	requesttiming.SetBytes(req.Context(), "compression_saved_bytes", len(body)-len(compressed))
	requesttiming.SetMs(req.Context(), "compression", int(time.Since(startedAt).Milliseconds()))
	return req, body, nil
}
