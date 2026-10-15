package ingestion

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

type responseStep struct {
	status  int
	headers http.Header
	body    string
	err     error
}

type scriptedTransport struct {
	steps []responseStep
	calls int
}

func (s *scriptedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	index := s.calls
	s.calls++
	if index >= len(s.steps) {
		return nil, errors.New("unexpected request")
	}
	step := s.steps[index]
	if step.err != nil {
		return nil, step.err
	}
	return &http.Response{
		StatusCode: step.status,
		Header:     step.headers,
		Body:       io.NopCloser(strings.NewReader(step.body)),
		Request:    request,
	}, nil
}

func TestRequestRetriesRateLimitsAndTemporaryFailures(t *testing.T) {
	tests := []struct {
		name      string
		source    RemoteSource
		steps     []responseStep
		wantCalls int
		wantWaits []time.Duration
		wantCode  string
		wantRetry time.Duration
	}{
		{
			name: "429 retry-after", source: HackerNews,
			steps:     []responseStep{{status: 429, headers: http.Header{"Retry-After": []string{"5"}}}, {status: 200, body: `{}`}},
			wantCalls: 2, wantWaits: []time.Duration{5 * time.Second},
		},
		{
			name: "github rate-limit 403", source: GitHub,
			steps:     []responseStep{{status: 403, headers: http.Header{"X-Ratelimit-Remaining": []string{"0"}, "X-Ratelimit-Reset": []string{"1792051207"}}}, {status: 200, body: `{}`}},
			wantCalls: 2, wantWaits: []time.Duration{7 * time.Second},
		},
		{
			name: "github exhausted rate-limit 403", source: GitHub,
			steps: []responseStep{
				{status: 403, headers: http.Header{"X-Ratelimit-Remaining": []string{"0"}, "X-Ratelimit-Reset": []string{"1792051207"}}},
				{status: 403, headers: http.Header{"X-Ratelimit-Remaining": []string{"0"}}},
				{status: 403, headers: http.Header{"X-Ratelimit-Remaining": []string{"0"}}},
			},
			wantCalls: 3, wantWaits: []time.Duration{7 * time.Second, 2 * time.Second}, wantCode: "RATE_LIMITED", wantRetry: 4 * time.Second,
		},
		{
			name: "ordinary forbidden", source: GitHub,
			steps:     []responseStep{{status: 403, body: `secret upstream body`}},
			wantCalls: 1, wantCode: "FORBIDDEN",
		},
		{
			name: "three temporary attempts", source: HackerNews,
			steps:     []responseStep{{status: 503}, {err: errors.New("temporary secret")}, {status: 500, body: `private body`}},
			wantCalls: 3, wantWaits: []time.Duration{time.Second, 2 * time.Second}, wantCode: "UPSTREAM_UNAVAILABLE", wantRetry: 4 * time.Second,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
			clock := now
			var waits []time.Duration
			transport := &scriptedTransport{steps: test.steps}
			controller := requestController{
				client: &http.Client{Transport: transport}, source: test.source,
				now: func() time.Time { return clock }, deadline: now.Add(5 * time.Minute),
				wait: func(_ context.Context, delay time.Duration) error {
					waits = append(waits, delay)
					clock = clock.Add(delay)
					return nil
				},
				jitter: func(time.Duration) time.Duration { return 0 }, requestTimeout: 15 * time.Second,
			}
			request, _ := http.NewRequest(http.MethodGet, fixedBase(test.source)+"/test", nil)
			var payload map[string]any
			report, err := controller.JSON(context.Background(), request, RequestGeneral, &payload)
			if report.Attempts != test.wantCalls || transport.calls != test.wantCalls {
				t.Fatalf("attempts=%d calls=%d", report.Attempts, transport.calls)
			}
			if len(waits) != len(test.wantWaits) {
				t.Fatalf("waits=%v want=%v", waits, test.wantWaits)
			}
			for index := range waits {
				if waits[index] != test.wantWaits[index] {
					t.Fatalf("waits=%v want=%v", waits, test.wantWaits)
				}
			}
			if test.wantCode == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || remoteErrorCode(err) != test.wantCode || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private") {
				t.Fatalf("err=%v code=%s", err, remoteErrorCode(err))
			}
			if test.wantRetry > 0 {
				if report.NextRetryAt == nil || !report.NextRetryAt.Equal(clock.Add(test.wantRetry)) {
					t.Fatalf("next retry=%v want=%v", report.NextRetryAt, clock.Add(test.wantRetry))
				}
			}
		})
	}
}

func TestRequestStopsWhenRequiredDelayExceedsTaskDeadline(t *testing.T) {
	now := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	transport := &scriptedTransport{steps: []responseStep{{status: 429, headers: http.Header{"Retry-After": []string{"600"}}}}}
	waited := false
	controller := requestController{
		client: &http.Client{Transport: transport}, source: HackerNews,
		now: func() time.Time { return now }, deadline: now.Add(5 * time.Minute),
		wait:   func(context.Context, time.Duration) error { waited = true; return nil },
		jitter: func(time.Duration) time.Duration { return 0 }, requestTimeout: 15 * time.Second,
	}
	request, _ := http.NewRequest(http.MethodGet, hackerNewsAPIBase+"/test", nil)
	report, err := controller.JSON(context.Background(), request, RequestGeneral, &map[string]any{})
	if remoteErrorCode(err) != "RATE_LIMITED" || report.Attempts != 1 || report.NextRetryAt == nil || !report.NextRetryAt.Equal(now.Add(10*time.Minute)) || waited {
		t.Fatalf("report=%+v err=%v waited=%v", report, err, waited)
	}
}

func TestRequestRetryUsesHTTPRetryAfterAndInjectedClock(t *testing.T) {
	now := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	clock := now
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "4")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	controller := requestController{
		client: &http.Client{Transport: &rewriteTransport{target: target}}, source: HackerNews,
		now: func() time.Time { return clock }, deadline: now.Add(5 * time.Minute),
		wait:   func(_ context.Context, delay time.Duration) error { clock = clock.Add(delay); return nil },
		jitter: func(time.Duration) time.Duration { return 0 }, requestTimeout: 15 * time.Second,
	}
	request, _ := http.NewRequest(http.MethodGet, hackerNewsAPIBase+"/test.json", nil)
	report, err := controller.JSON(context.Background(), request, RequestGeneral, &map[string]any{})
	if err != nil || report.Attempts != 2 || calls != 2 || !clock.Equal(now.Add(4*time.Second)) {
		t.Fatalf("report=%+v calls=%d clock=%v err=%v", report, calls, clock, err)
	}
}

func TestRequestClaimsPersistentBudgetForEveryAttempt(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	clock := now
	run := createQueuedRun(t, db, GitHub, now)
	transport := &scriptedTransport{steps: []responseStep{{status: 503}, {status: 200, body: `{}`}}}
	controller := requestController{
		db: db, runID: run.ID, client: &http.Client{Transport: transport}, source: GitHub,
		budget: &BudgetPolicy{HourlyLimit: 60, SearchMinuteLimit: 10},
		now:    func() time.Time { return clock }, deadline: now.Add(5 * time.Minute),
		wait:   func(_ context.Context, delay time.Duration) error { clock = clock.Add(delay); return nil },
		jitter: func(time.Duration) time.Duration { return 0 }, requestTimeout: 15 * time.Second,
	}
	request, _ := http.NewRequest(http.MethodGet, githubAPIBase+"/test", nil)
	report, err := controller.JSON(context.Background(), request, RequestGeneral, &map[string]any{})
	if err != nil || report.Attempts != 2 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	var claims int
	if err = db.QueryRow(`SELECT count(*) FROM request_budget_claims WHERE run_id=?`, run.ID).Scan(&claims); err != nil || claims != 2 {
		t.Fatalf("claims=%d err=%v", claims, err)
	}
}

func TestRequestReplaysBodyForEveryAttempt(t *testing.T) {
	now := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	clock := now
	var bodies []string
	calls := 0
	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, string(body))
		calls++
		status := http.StatusServiceUnavailable
		payload := ""
		if calls == 2 {
			status = http.StatusOK
			payload = `{}`
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(payload)), Request: request}, nil
	})
	controller := requestController{
		client: &http.Client{Transport: transport}, source: ProductHunt, now: func() time.Time { return clock },
		deadline: now.Add(5 * time.Minute), wait: func(_ context.Context, delay time.Duration) error { clock = clock.Add(delay); return nil },
		jitter: func(time.Duration) time.Duration { return 0 }, requestTimeout: 15 * time.Second,
	}
	request, _ := http.NewRequest(http.MethodPost, "https://api.producthunt.com/v2/api/graphql", strings.NewReader(`{"query":"fixed"}`))
	report, err := controller.JSON(context.Background(), request, RequestGeneral, &map[string]any{})
	if err != nil || report.Attempts != 2 || len(bodies) != 2 || bodies[0] != bodies[1] {
		t.Fatalf("report=%+v bodies=%q err=%v", report, bodies, err)
	}
}

func TestRequestEnforcesDecompressedBodyLimitAndTimeout(t *testing.T) {
	t.Run("gzip body", func(t *testing.T) {
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		if err := json.NewEncoder(writer).Encode(map[string]string{"value": strings.Repeat("x", maxRemoteResponseBytes)}); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Encoding": []string{"gzip"}}, Body: io.NopCloser(bytes.NewReader(compressed.Bytes())), Request: request, Uncompressed: false}, nil
		})
		controller := testRequestController(&http.Client{Transport: transport})
		request, _ := http.NewRequest(http.MethodGet, hackerNewsAPIBase+"/test", nil)
		_, err := controller.JSON(context.Background(), request, RequestGeneral, &map[string]any{})
		if remoteErrorCode(err) != "RESPONSE_TOO_LARGE" {
			t.Fatalf("err=%v", err)
		}
	})

	t.Run("timeout cancels transport", func(t *testing.T) {
		transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			<-request.Context().Done()
			return nil, request.Context().Err()
		})
		controller := testRequestController(&http.Client{Transport: transport})
		controller.requestTimeout = 10 * time.Millisecond
		controller.deadline = time.Now().Add(20 * time.Millisecond)
		request, _ := http.NewRequest(http.MethodGet, hackerNewsAPIBase+"/test", nil)
		report, err := controller.JSON(context.Background(), request, RequestGeneral, &map[string]any{})
		if remoteErrorCode(err) != "NETWORK_ERROR" || report.Attempts != 1 {
			t.Fatalf("report=%+v err=%v", report, err)
		}
	})
}

func TestGitHubResponseBudgetHeadersArePersisted(t *testing.T) {
	db := openTestDB(t)
	now := time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC)
	run := createQueuedRun(t, db, GitHub, now)
	reset := now.Add(20 * time.Minute)
	transport := &scriptedTransport{steps: []responseStep{{status: 200, body: `{}`, headers: http.Header{
		"X-Ratelimit-Remaining": []string{"0"}, "X-Ratelimit-Reset": []string{strconv.FormatInt(reset.Unix(), 10)},
	}}}}
	controller := requestController{
		db: db, runID: run.ID, client: &http.Client{Transport: transport}, source: GitHub,
		budget: &BudgetPolicy{HourlyLimit: 60, SearchMinuteLimit: 10}, now: func() time.Time { return now },
		deadline: now.Add(5 * time.Minute), wait: waitWithTimer, jitter: func(time.Duration) time.Duration { return 0 }, requestTimeout: 15 * time.Second,
	}
	request, _ := http.NewRequest(http.MethodGet, githubAPIBase+"/test", nil)
	if _, err := controller.JSON(context.Background(), request, RequestGeneral, &map[string]any{}); err != nil {
		t.Fatal(err)
	}
	_, err := ClaimRequestBudget(context.Background(), db, run.ID, GitHub, RequestGeneral, *controller.budget, now.Add(time.Minute))
	var limited *RateLimitError
	if !errors.As(err, &limited) || !limited.RetryAt.Equal(reset) {
		t.Fatalf("provider budget not persisted: err=%v limited=%+v", err, limited)
	}
}

func testRequestController(client *http.Client) requestController {
	now := time.Now()
	return requestController{
		client: client, source: HackerNews, now: time.Now, deadline: now.Add(5 * time.Minute),
		wait: waitWithTimer, jitter: func(time.Duration) time.Duration { return 0 }, requestTimeout: 15 * time.Second,
	}
}

func fixedBase(source RemoteSource) string {
	if source == GitHub {
		return githubAPIBase
	}
	return hackerNewsAPIBase
}
