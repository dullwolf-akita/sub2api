package apicompat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// OpenAI Chat Completions → Responses → Anthropic must emit top-level automatic
// cache_control so API Key upstreams populate cache_*_input_tokens (#8285).
func TestResponsesToAnthropicRequest_SetsTopLevelEphemeralCacheControl(t *testing.T) {
	out, err := ResponsesToAnthropicRequest(&ResponsesRequest{
		Model: "claude-sonnet-4-5",
		Input: json.RawMessage(`"hello"`),
	})
	require.NoError(t, err)
	require.NotNil(t, out.CacheControl)
	require.Equal(t, "ephemeral", out.CacheControl.Type)
	require.Empty(t, out.CacheControl.TTL)

	body, err := json.Marshal(out)
	require.NoError(t, err)
	var wire map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &wire))
	require.JSONEq(t, `{"type":"ephemeral"}`, string(wire["cache_control"]))
}

func TestChatCompletionsToAnthropic_SetsTopLevelEphemeralCacheControl(t *testing.T) {
	respReq, err := ChatCompletionsToResponses(&ChatCompletionsRequest{
		Model: "claude-sonnet-4-5",
		Messages: []ChatMessage{
			{Role: "system", Content: json.RawMessage(`"You are a long stable system prompt for caching."`)},
			{Role: "user", Content: json.RawMessage(`"ping"`)},
		},
	})
	require.NoError(t, err)
	out, err := ResponsesToAnthropicRequest(respReq)
	require.NoError(t, err)
	require.NotNil(t, out.CacheControl)
	require.Equal(t, "ephemeral", out.CacheControl.Type)

	body, err := json.Marshal(out)
	require.NoError(t, err)
	require.Contains(t, string(body), `"cache_control":{"type":"ephemeral"}`)
}
