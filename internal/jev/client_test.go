package jev

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var testQuestion = Question{
	Instructions: "pick an agent",
	Criteria:     map[string]string{"codex-fast": "quick edits", "claude-hard": "multi-file work"},
}

func init() { retryDelay = 0 }

func routeReply(choice string, confidence float64) string {
	conf, _ := json.Marshal(confidence)
	return `{"model":"typesafe/jev-1.13-20260917","answers":{"route":{"type":"choice","choice":"` + choice +
		`","confidence":` + string(conf) + `}},"usage":{"cost":0.0004}}`
}

func newTestServer(t *testing.T, handler func(n int32, w http.ResponseWriter, r *http.Request)) (*Client, *int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler(atomic.AddInt32(&calls, 1), w, r)
	}))
	t.Cleanup(srv.Close)
	return NewClient(srv.URL+"/systemone", "sk-test", DefaultModel, DefaultThreshold, &http.Client{Timeout: 200 * time.Millisecond}), &calls
}

func TestChooseOK(t *testing.T) {
	var body map[string]any
	c, calls := newTestServer(t, func(_ int32, w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-test" || r.URL.Path != "/systemone" {
			t.Errorf("bad request: %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = io.WriteString(w, routeReply("claude-hard", 0.86))
	})
	a, err := c.Choose(context.Background(), map[string]any{"prompt": "fix it <a> & b"}, testQuestion)
	if err != nil {
		t.Fatal(err)
	}
	if a.Choice != "claude-hard" || a.Confidence != 0.86 || a.Cost != 0.0004 || atomic.LoadInt32(calls) != 1 {
		t.Fatalf("answer = %+v calls=%d", a, atomic.LoadInt32(calls))
	}
	if body["model"] != DefaultModel {
		t.Errorf("model = %v", body["model"])
	}
	if s, ok := body["state"].(string); !ok || !strings.Contains(s, "fix it <a> & b") {
		t.Errorf("state must be a JSON string: %v", body["state"])
	}
	q := body["questions"].(map[string]any)["route"].(map[string]any)
	if q["type"] != "choice" || q["criteria"].(map[string]any)["codex-fast"] != "quick edits" {
		t.Errorf("question = %v", q)
	}
}

func TestChooseRetriesOnce(t *testing.T) {
	c, calls := newTestServer(t, func(n int32, w http.ResponseWriter, _ *http.Request) {
		if n == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, routeReply("codex-fast", 0.7))
	})
	a, err := c.Choose(context.Background(), nil, testQuestion)
	if err != nil || a.Choice != "codex-fast" || atomic.LoadInt32(calls) != 2 {
		t.Fatalf("a=%+v err=%v calls=%d", a, err, atomic.LoadInt32(calls))
	}
}

func TestChooseServerErrorTwice(t *testing.T) {
	c, calls := newTestServer(t, func(_ int32, w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	if _, err := c.Choose(context.Background(), nil, testQuestion); err == nil || atomic.LoadInt32(calls) != 2 {
		t.Fatalf("err=%v calls=%d", err, atomic.LoadInt32(calls))
	}
}

func TestChooseNoRetryOn4xx(t *testing.T) {
	c, calls := newTestServer(t, func(_ int32, w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	if _, err := c.Choose(context.Background(), nil, testQuestion); err == nil || atomic.LoadInt32(calls) != 1 {
		t.Fatalf("err=%v calls=%d", err, atomic.LoadInt32(calls))
	}
}

func TestChooseBadShape(t *testing.T) {
	for name, reply := range map[string]string{
		"not json":       `nope`,
		"no answer":      `{"answers":{}}`,
		"wrong type":     `{"answers":{"route":{"type":"noul","noul":0.5}}}`,
		"no confidence":  `{"answers":{"route":{"type":"choice","choice":"codex-fast"}}}`,
		"confidence > 1": `{"answers":{"route":{"type":"choice","choice":"codex-fast","confidence":1.5}}}`,
		"unknown choice": routeReply("rm-rf", 0.9),
	} {
		t.Run(name, func(t *testing.T) {
			c, _ := newTestServer(t, func(_ int32, w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, reply) })
			if _, err := c.Choose(context.Background(), nil, testQuestion); !errors.Is(err, ErrContract) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestChooseTimeoutRetriesThenFails(t *testing.T) {
	c, calls := newTestServer(t, func(_ int32, _ http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	})
	if _, err := c.Choose(context.Background(), nil, testQuestion); err == nil || atomic.LoadInt32(calls) != 2 {
		t.Fatalf("err=%v calls=%d", err, atomic.LoadInt32(calls))
	}
}

func TestChooseRecordedReplyShape(t *testing.T) {
	raw, err := os.ReadFile("testdata/jev_response_sample.json")
	if err != nil {
		t.Fatal(err)
	}
	sample := strings.Replace(string(raw), `"intent"`, `"route"`, 1)
	q := Question{Instructions: "x", Criteria: map[string]string{"coding_task": "a", "read_only_question": "b", "ambiguous": "c"}}
	c, _ := newTestServer(t, func(_ int32, w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, sample) })
	a, err := c.Choose(context.Background(), nil, q)
	if err != nil || a.Choice != "coding_task" || a.Confidence != 1 || a.Cost != 2.1546e-05 || a.Model != "typesafe/jev-1.13-20260917" {
		t.Fatalf("a=%+v err=%v", a, err)
	}
}

func TestChooseNilStateSendsEmptyObject(t *testing.T) {
	var state any
	c, _ := newTestServer(t, func(_ int32, w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		state = body["state"]
		_, _ = io.WriteString(w, routeReply("codex-fast", 0.9))
	})
	if _, err := c.Choose(context.Background(), nil, testQuestion); err != nil {
		t.Fatal(err)
	}
	if state != "{}" {
		t.Fatalf("state = %v", state)
	}
}

func TestChooseCallerCancelNotRetried(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c, calls := newTestServer(t, func(_ int32, _ http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		cancel()
		select {
		case <-r.Context().Done():
		case <-time.After(time.Second):
		}
	})
	_, err := c.Choose(ctx, nil, testQuestion)
	if !errors.Is(err, context.Canceled) || atomic.LoadInt32(calls) != 1 {
		t.Fatalf("err=%v calls=%d", err, atomic.LoadInt32(calls))
	}
}
