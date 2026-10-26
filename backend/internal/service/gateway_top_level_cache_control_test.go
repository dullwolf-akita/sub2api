package service

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestDropTopLevelCacheControlIfExplicitBreakpoints(t *testing.T) {
	t.Run("keeps top-level when no block breakpoints", func(t *testing.T) {
		body := []byte(`{"cache_control":{"type":"ephemeral"},"messages":[{"role":"user","content":"hi"}]}`)
		out := dropTopLevelCacheControlIfExplicitBreakpoints(body)
		require.True(t, gjson.GetBytes(out, "cache_control").Exists())
		require.Equal(t, "ephemeral", gjson.GetBytes(out, "cache_control.type").String())
	})

	t.Run("drops top-level when block breakpoints exist", func(t *testing.T) {
		body := []byte(`{"cache_control":{"type":"ephemeral"},"system":[{"type":"text","text":"sys","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`)
		out := dropTopLevelCacheControlIfExplicitBreakpoints(body)
		require.False(t, gjson.GetBytes(out, "cache_control").Exists())
		require.True(t, gjson.GetBytes(out, "system.0.cache_control").Exists())
	})
}

func TestEnforceCacheControlLimit_TopLevelAlonePreserved(t *testing.T) {
	body := []byte(`{"alpha":1,"cache_control":{"type":"ephemeral"},"messages":[{"role":"user","content":"hi"}],"omega":2}`)
	out := enforceCacheControlLimit(body)
	require.True(t, gjson.GetBytes(out, "cache_control").Exists())
	require.Equal(t, "ephemeral", gjson.GetBytes(out, "cache_control.type").String())
	require.Equal(t, 1, strings.Count(string(out), `"cache_control"`))
}

func TestEnforceCacheControlLimit_DropsTopLevelWhenAlreadyAtFourBlocks(t *testing.T) {
	// 4 block-level + top-level would be Found 5 upstream; drop top-level.
	body := []byte(`{"cache_control":{"type":"ephemeral"},"system":[{"type":"text","text":"s1","cache_control":{"type":"ephemeral"}},{"type":"text","text":"s2","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":[{"type":"text","text":"m1","cache_control":{"type":"ephemeral"}},{"type":"text","text":"m2","cache_control":{"type":"ephemeral"}}]}],"tools":[{"name":"a","input_schema":{},"cache_control":{"type":"ephemeral"}}]}`)
	out := enforceCacheControlLimit(body)
	require.False(t, gjson.GetBytes(out, "cache_control").Exists(), "top-level must be dropped when explicit breakpoints exist")
	_, messagePaths, toolPaths, systemPaths := collectCacheControlPaths(out)
	total := len(messagePaths) + len(toolPaths) + len(systemPaths)
	require.LessOrEqual(t, total, maxCacheControlBlocks)
}
