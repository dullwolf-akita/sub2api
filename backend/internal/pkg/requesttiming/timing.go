package requesttiming

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

type contextKey struct{}

// Breakdown is persisted with a usage log. Values are elapsed milliseconds
// from the request timing start, rather than wall-clock timestamps.
type Breakdown struct {
	RequestReceivedMs int  `json:"request_received_ms"`
	RequestBodyReadMs *int `json:"request_body_read_ms,omitempty"`
	AuthMs            *int `json:"auth_ms,omitempty"`
	RoutingMs         *int `json:"routing_ms,omitempty"`
	QueueWaitMs       *int `json:"queue_wait_ms,omitempty"`
	UpstreamRequestMs *int `json:"upstream_request_ms,omitempty"`
	UpstreamTTFBMs    *int `json:"upstream_ttfb_ms,omitempty"`
	FirstTokenMs      *int `json:"first_token_ms,omitempty"`
	UpstreamTotalMs   *int `json:"upstream_total_ms,omitempty"`
	ResponseWriteMs   *int `json:"response_write_ms,omitempty"`
	TotalMs           int  `json:"total_ms"`
}

type Timing struct {
	startedAt time.Time
	mu        sync.Mutex
	values    Breakdown
}

func New(startedAt time.Time) *Timing {
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	return &Timing{startedAt: startedAt}
}

func With(ctx context.Context, timing *Timing) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, contextKey{}, timing)
}

// Propagate copies the request timing object into a worker context. Usage
// records are written asynchronously, so the worker cannot use the original
// request context directly.
func Propagate(parent, base context.Context) context.Context {
	timing, ok := FromContext(parent)
	if !ok {
		return base
	}
	return With(base, timing)
}

func FromContext(ctx context.Context) (*Timing, bool) {
	if ctx == nil {
		return nil, false
	}
	t, ok := ctx.Value(contextKey{}).(*Timing)
	return t, ok && t != nil
}

func Mark(ctx context.Context, field string) {
	t, ok := FromContext(ctx)
	if !ok {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	v := int(time.Since(t.startedAt).Milliseconds())
	set := func(dst **int) {
		if *dst == nil {
			*dst = &v
		}
	}
	switch field {
	case "request_body_read":
		set(&t.values.RequestBodyReadMs)
	case "auth":
		set(&t.values.AuthMs)
	case "routing":
		set(&t.values.RoutingMs)
	case "queue_wait":
		set(&t.values.QueueWaitMs)
	case "upstream_request":
		set(&t.values.UpstreamRequestMs)
	case "upstream_ttfb":
		set(&t.values.UpstreamTTFBMs)
	case "first_token":
		set(&t.values.FirstTokenMs)
	case "upstream_total":
		set(&t.values.UpstreamTotalMs)
	case "response_write":
		set(&t.values.ResponseWriteMs)
	}
}

// SetMs records a stage whose elapsed value was measured by a protocol parser.
func SetMs(ctx context.Context, field string, value int) {
	t, ok := FromContext(ctx)
	if !ok || value < 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	set := func(dst **int) {
		if *dst == nil {
			v := value
			*dst = &v
		}
	}
	switch field {
	case "auth":
		set(&t.values.AuthMs)
	case "routing":
		set(&t.values.RoutingMs)
	case "queue_wait":
		set(&t.values.QueueWaitMs)
	case "first_token":
		set(&t.values.FirstTokenMs)
	}
}

func Snapshot(ctx context.Context) *Breakdown {
	t, ok := FromContext(ctx)
	if !ok {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.values
	v.TotalMs = int(time.Since(t.startedAt).Milliseconds())
	return &v
}

func JSON(ctx context.Context) []byte {
	v := Snapshot(ctx)
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}
