package service

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"
)

func TestPrepareUpstreamRequestDefaultsToIdentity(t *testing.T) {
	body := bytes.Repeat([]byte("a"), upstreamRequestCompressionThreshold)
	req, err := http.NewRequest(http.MethodPost, "https://example.com/v1/messages", bytes.NewReader(body))
	require.NoError(t, err)

	_, _, err = PrepareUpstreamRequest(req, body, &Account{Extra: map[string]any{}})
	require.NoError(t, err)
	require.Empty(t, req.Header.Get("Content-Encoding"))
	require.Equal(t, int64(len(body)), req.ContentLength)
}

func TestPrepareUpstreamRequestZstdThresholdAndBody(t *testing.T) {
	account := &Account{Extra: map[string]any{"upstream_request_compression": "zstd"}}
	smallBody := bytes.Repeat([]byte("a"), upstreamRequestCompressionThreshold-1)
	smallReq, err := http.NewRequest(http.MethodPost, "https://example.com/v1/messages", bytes.NewReader(smallBody))
	require.NoError(t, err)
	_, _, err = PrepareUpstreamRequest(smallReq, smallBody, account)
	require.NoError(t, err)
	require.Empty(t, smallReq.Header.Get("Content-Encoding"))

	body := bytes.Repeat([]byte(`{"input":"compressible"}`), upstreamRequestCompressionThreshold/24+1)
	req, err := http.NewRequest(http.MethodPost, "https://example.com/v1/messages", bytes.NewReader(body))
	require.NoError(t, err)
	_, returnedBody, err := PrepareUpstreamRequest(req, body, account)
	require.NoError(t, err)
	require.Equal(t, body, returnedBody)
	require.Equal(t, "zstd", req.Header.Get("Content-Encoding"))
	require.Less(t, req.ContentLength, int64(len(body)))

	compressed, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	decoder, err := zstd.NewReader(nil)
	require.NoError(t, err)
	decompressed, err := decoder.DecodeAll(compressed, nil)
	require.NoError(t, err)
	require.Equal(t, body, decompressed)
}
