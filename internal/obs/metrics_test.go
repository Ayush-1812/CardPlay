package obs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestExpositionFormat(t *testing.T) {
	c := NewCounter("test_events_total", "Events.", "kind")
	c.Inc(`a"b`)
	c.Add(2, `a"b`)
	c.Inc("x", "extra") // wrong label count is ignored
	h := NewHistogram("test_seconds", "Durations.", []float64{0.1, 1})
	h.Observe(0.05)
	h.Observe(0.5)
	h.Observe(3)
	NewGauge("test_gauge", "A gauge.", func() float64 { return 7 })
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/metrics", nil))
	out := rec.Body.String()
	for _, want := range []string{
		"# TYPE test_events_total counter\ntest_events_total{kind=\"a\\\"b\"} 3\n",
		`test_seconds_bucket{le="0.1"} 1`,
		`test_seconds_bucket{le="1"} 2`,
		`test_seconds_bucket{le="+Inf"} 3`,
		"test_seconds_sum 3.55",
		"test_seconds_count 3",
		"test_gauge 7",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestInstrumentAndRecover(t *testing.T) {
	r := chi.NewRouter()
	r.Use(Instrument(), Recover)
	r.Get("/items/{id}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	r.Get("/boom", func(http.ResponseWriter, *http.Request) { panic("boom") })
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/items/secret-id?token=t", nil))
	if HTTPRequests.Value("GET", "/items/{id}", "2xx") != 1 {
		t.Fatal("request not counted by route pattern")
	}
	panics := Panics.Value()
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest("GET", "/boom", nil))
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), "INTERNAL") || Panics.Value() != panics+1 {
		t.Fatalf("panic handling: %d %s", rec.Code, rec.Body.String())
	}
	if HTTPRequests.Value("GET", "/boom", "5xx") != 1 {
		t.Fatal("panic not recorded as 5xx")
	}
}
