package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestBPrimeResponseHeadersAndLog(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer upstream.Close()

	var logs syncBuffer
	handler, err := newProxy(Config{
		Upstream:      upstream.URL,
		Token:         "test-gateway-token",
		UpstreamToken: "test-upstream-token",
		Models:        map[string]string{"private-model": "target-model"},
		ChunkRunes:    24,
		MaxConcurrent: -1,
		Timeout:       30 * time.Second,
		Logger:        slog.New(slog.NewTextHandler(&logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(handler)
	defer gw.Close()

	post := func(text string) *http.Response {
		payload := map[string]any{
			"model":    "private-model",
			"messages": []map[string]string{{"role": "user", "content": text}},
		}
		b, _ := json.Marshal(payload)
		req, _ := http.NewRequest("POST", gw.URL+"/v1/chat/completions", bytes.NewReader(b))
		req.Header.Set("Authorization", "Bearer test-gateway-token")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp
	}

	normal := post("<past_observe>PLAINTEXT_NORMAL</past_observe>")
	if normal.Header.Get("X-InputCheck-Transform") != "encoded" {
		t.Fatalf("normal status=%s", normal.Header.Get("X-InputCheck-Transform"))
	}
	if normal.Header.Get("X-InputCheck-Ambiguous") != "0" || normal.Header.Get("X-InputCheck-Partial") != "0" {
		t.Fatalf("normal markers: amb=%s partial=%s", normal.Header.Get("X-InputCheck-Ambiguous"), normal.Header.Get("X-InputCheck-Partial"))
	}
	mu.Lock()
	normalBody := ""
	if len(bodies) > 0 {
		normalBody = bodies[0]
	}
	mu.Unlock()
	if strings.Contains(normalBody, "PLAINTEXT_NORMAL") || !strings.Contains(normalBody, "[[OBF-B64/1") {
		t.Fatal("normal upstream body was not fully encoded")
	}
	if !strings.Contains(normalBody, `"model":"target-model"`) {
		t.Fatal("model mapping lost")
	}

	partial := post("<past_observe>PLAINTEXT_AMBIG</past_observe>" + strings.Repeat("Z", 2000) + "</past_observe>")
	if partial.Header.Get("X-InputCheck-Transform") != "encoded" {
		t.Fatalf("partial status=%s", partial.Header.Get("X-InputCheck-Transform"))
	}
	if partial.Header.Get("X-InputCheck-Ambiguous") != "1" || partial.Header.Get("X-InputCheck-Partial") != "1" {
		t.Fatalf("partial markers missing: amb=%s partial=%s", partial.Header.Get("X-InputCheck-Ambiguous"), partial.Header.Get("X-InputCheck-Partial"))
	}

	var logText string
	for i := 0; i < 80; i++ {
		logText = logs.String()
		lines := strings.Split(logText, "\n")
		found := false
		for _, line := range lines {
			if strings.Contains(line, "request_complete") && strings.Contains(line, "partial=1") {
				found = true
				break
			}
		}
		if found {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !strings.Contains(logText, "ambiguous=1") || !strings.Contains(logText, "partial=1") {
		t.Fatalf("request_complete log missing markers: %s", logText)
	}
	mu.Lock()
	calls := len(bodies)
	mu.Unlock()
	t.Logf("normal encoded, partial marked in headers and log; upstream calls=%d", calls)
}
