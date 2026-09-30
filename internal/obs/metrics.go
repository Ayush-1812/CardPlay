// Package obs provides structured logging, Prometheus-format metrics and
// HTTP instrumentation without third-party dependencies.
package obs

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type metric interface {
	write(w io.Writer)
}

var (
	registryMu sync.Mutex
	registry   []metric
)

func register(m metric) {
	registryMu.Lock()
	registry = append(registry, m)
	registryMu.Unlock()
}

func escape(v string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(v)
}

func labelString(names, values []string, extra ...string) string {
	parts := make([]string, 0, len(names)+1)
	for i, n := range names {
		parts = append(parts, fmt.Sprintf(`%s="%s"`, n, escape(values[i])))
	}
	parts = append(parts, extra...)
	if len(parts) == 0 {
		return ""
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func formatFloat(v float64) string {
	if math.IsInf(v, 1) {
		return "+Inf"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

// Counter is a monotonically increasing count with optional labels.
type Counter struct {
	name, help string
	labels     []string
	mu         sync.Mutex
	values     map[string]float64
	order      map[string][]string
}

func NewCounter(name, help string, labels ...string) *Counter {
	c := &Counter{name: name, help: help, labels: labels, values: map[string]float64{}, order: map[string][]string{}}
	register(c)
	return c
}

// Inc adds one for the given label values (in declaration order).
func (c *Counter) Inc(values ...string) { c.Add(1, values...) }

func (c *Counter) Add(v float64, values ...string) {
	if len(values) != len(c.labels) {
		return
	}
	key := strings.Join(values, "\xff")
	c.mu.Lock()
	c.values[key] += v
	c.order[key] = values
	c.mu.Unlock()
}

// Value returns the current count, for tests.
func (c *Counter) Value(values ...string) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.values[strings.Join(values, "\xff")]
}

func (c *Counter) write(w io.Writer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n", c.name, c.help, c.name)
	keys := make([]string, 0, len(c.values))
	for k := range c.values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "%s%s %s\n", c.name, labelString(c.labels, c.order[k]), formatFloat(c.values[k]))
	}
}

// Histogram records observations in cumulative buckets.
type Histogram struct {
	name, help string
	labels     []string
	buckets    []float64
	mu         sync.Mutex
	series     map[string]*histSeries
}

type histSeries struct {
	values []string
	counts []uint64
	sum    float64
	count  uint64
}

// LatencyBuckets suit request and command durations in seconds.
var LatencyBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

func NewHistogram(name, help string, buckets []float64, labels ...string) *Histogram {
	h := &Histogram{name: name, help: help, labels: labels, buckets: buckets, series: map[string]*histSeries{}}
	register(h)
	return h
}

func (h *Histogram) Observe(v float64, values ...string) {
	if len(values) != len(h.labels) {
		return
	}
	key := strings.Join(values, "\xff")
	h.mu.Lock()
	defer h.mu.Unlock()
	s := h.series[key]
	if s == nil {
		s = &histSeries{values: values, counts: make([]uint64, len(h.buckets))}
		h.series[key] = s
	}
	for i, b := range h.buckets {
		if v <= b {
			s.counts[i]++
		}
	}
	s.sum += v
	s.count++
}

func (h *Histogram) write(w io.Writer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s histogram\n", h.name, h.help, h.name)
	keys := make([]string, 0, len(h.series))
	for k := range h.series {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s := h.series[k]
		for i, b := range h.buckets {
			fmt.Fprintf(w, "%s_bucket%s %d\n", h.name, labelString(h.labels, s.values, fmt.Sprintf(`le="%s"`, formatFloat(b))), s.counts[i])
		}
		fmt.Fprintf(w, "%s_bucket%s %d\n", h.name, labelString(h.labels, s.values, `le="+Inf"`), s.count)
		fmt.Fprintf(w, "%s_sum%s %s\n", h.name, labelString(h.labels, s.values), formatFloat(s.sum))
		fmt.Fprintf(w, "%s_count%s %d\n", h.name, labelString(h.labels, s.values), s.count)
	}
}

// Gauge reports a value computed at scrape time.
type Gauge struct {
	name, help string
	fn         func() float64
}

func NewGauge(name, help string, fn func() float64) *Gauge {
	g := &Gauge{name: name, help: help, fn: fn}
	register(g)
	return g
}

func (g *Gauge) write(w io.Writer) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s %s\n", g.name, g.help, g.name, g.name, formatFloat(g.fn()))
}

// Handler serves every registered metric in the Prometheus text format.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		registryMu.Lock()
		metrics := append([]metric(nil), registry...)
		registryMu.Unlock()
		for _, m := range metrics {
			m.write(w)
		}
	})
}

// Application metrics. Labels never carry user content or card data.
var (
	HTTPRequests        = NewCounter("cardplay_http_requests_total", "HTTP requests by route and status class.", "method", "route", "code")
	HTTPDuration        = NewHistogram("cardplay_http_request_duration_seconds", "HTTP request duration by route.", LatencyBuckets, "route")
	WSMessages          = NewCounter("cardplay_ws_messages_total", "WebSocket messages received by type.", "type")
	GameCommands        = NewCounter("cardplay_game_commands_total", "Game commands by result.", "result")
	GameCommandDuration = NewHistogram("cardplay_game_command_duration_seconds", "Time to apply and durably commit a game command.", LatencyBuckets)
	StatePushes         = NewCounter("cardplay_match_state_pushes_total", "Per-player match projections sent.")
	Logins              = NewCounter("cardplay_logins_total", "Login attempts by result.", "result")
	RateLimited         = NewCounter("cardplay_rate_limited_total", "Requests refused by a rate limit.", "scope")
	ChatMessages        = NewCounter("cardplay_chat_messages_total", "Chat messages accepted.")
	Notifications       = NewCounter("cardplay_notifications_total", "Change notifications received from PostgreSQL.", "kind")
	Panics              = NewCounter("cardplay_panics_total", "Recovered handler panics.")
	ClientErrors        = NewCounter("cardplay_client_errors_total", "Browser errors reported by the web client.")
)
