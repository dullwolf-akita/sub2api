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
	RequestReceivedMs     int  `json:"request_received_ms"`
	RequestBodyReadMs     *int `json:"request_body_read_ms,omitempty"`
	AuthMs                *int `json:"auth_ms,omitempty"`
	RoutingMs             *int `json:"routing_ms,omitempty"`
	QueueWaitMs           *int `json:"queue_wait_ms,omitempty"`
	UpstreamRequestMs     *int `json:"upstream_request_ms,omitempty"`
	UpstreamTTFBMs        *int `json:"upstream_ttfb_ms,omitempty"`
	FirstTokenMs          *int `json:"first_token_ms,omitempty"`
	UpstreamBodyMs        *int `json:"upstream_body_ms,omitempty"`
	ClientResponseWriteMs *int `json:"client_response_write_ms,omitempty"`
	TotalMs               int  `json:"total_ms"`
}

type Timing struct {
	startedAt           time.Time
	mu                  sync.Mutex
	values              Breakdown
	requestBodyRead     time.Duration
	upstreamRequest     time.Duration
	upstreamTTFB        time.Duration
	upstreamBody        time.Duration
	clientResponseWrite time.Duration
	completedTotalMs    *int
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

func Active(ctx context.Context) bool {
	_, ok := FromContext(ctx)
	return ok
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
	case "upstream_request":
		set(&t.values.UpstreamRequestMs)
	case "upstream_ttfb":
		set(&t.values.UpstreamTTFBMs)
	case "first_token":
		set(&t.values.FirstTokenMs)
	}
}

// AddDuration accumulates repeated intervals without losing sub-millisecond
// writes. Snapshot converts the totals to milliseconds once.
func AddDuration(ctx context.Context, field string, value time.Duration) {
	t, ok := FromContext(ctx)
	if !ok || value < 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	switch field {
	case "request_body_read":
		t.requestBodyRead += value
	case "upstream_request":
		t.upstreamRequest += value
	case "upstream_ttfb":
		t.upstreamTTFB += value
	case "upstream_body":
		t.upstreamBody += value
	case "client_response_write":
		t.clientResponseWrite += value
	}
}

// Complete freezes total_ms before asynchronous usage persistence starts.
func Complete(ctx context.Context) {
	t, ok := FromContext(ctx)
	if !ok {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.completedTotalMs == nil {
		v := int(time.Since(t.startedAt).Milliseconds())
		t.completedTotalMs = &v
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
	setDuration := func(dst **int, value time.Duration) {
		if value > 0 {
			ms := int(value.Milliseconds())
			*dst = &ms
		}
	}
	setDuration(&v.RequestBodyReadMs, t.requestBodyRead)
	setDuration(&v.UpstreamRequestMs, t.upstreamRequest)
	setDuration(&v.UpstreamTTFBMs, t.upstreamTTFB)
	setDuration(&v.UpstreamBodyMs, t.upstreamBody)
	setDuration(&v.ClientResponseWriteMs, t.clientResponseWrite)
	if t.completedTotalMs != nil {
		v.TotalMs = *t.completedTotalMs
	} else {
		v.TotalMs = int(time.Since(t.startedAt).Milliseconds())
	}
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
