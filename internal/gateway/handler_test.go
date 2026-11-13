package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"inputcheck/internal/codec"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testConfig(url string) Config {
	return Config{Upstream: url, Token: "inbound-test-only", UpstreamToken: "upstream-test-only", Models: map[string]string{"混淆-flash": "flash"}, ChunkRunes: 24, MaxConcurrent: 2}
}
func call(h http.Handler, body string, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestProxyTransformsOnlyPermittedTextAndPreservesUnknownJSON(t *testing.T) {
	var seen []byte
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer upstream-test-only" || r.Header.Get("Cookie") != "" || r.URL.RawQuery != "" {
			t.Error("credential/header leak")
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Error("wrong path")
		}
		seen, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"model":"flash","choices":[],"usage":{"total_tokens":7}}`)
	}))
	defer up.Close()
	h, err := newProxy(testConfig(up.URL))
	if err != nil {
		t.Fatal(err)
	}
	input := `{"model":"混淆-flash","messages":[{"role":"system","content":"<user_input>system example</user_input>"},{"role":"user","content":"<用户输入>history</用户输入>"},{"role":"assistant","content":"<user_Input>assistant history</user_Input>","tool_calls":[{"function":{"arguments":"<user_input>args</user_input>"}}]},{"role":"tool","content":"<user_input>tool result</user_input>"},{"role":"user","content":[{"type":"text","text":"outside <user_input>甲\n\n乙</user_input>"},{"type":"image_url","image_url":{"url":"https://example.invalid/image","note":"<user_input>leave</user_input>"}}]}],"metadata":{"big":900719925474099312345,"opaque":"leave"},"stream":false,"response_format":{"type":"json_object"},"max_tokens":256}`
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(input))
	r.Header.Set("Authorization", "Bearer inbound-test-only")
	r.Header.Set("Cookie", "must-not-forward")
	r.Header.Set("X-Api-Key", "must-not-forward")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(string(seen), "900719925474099312345") {
		t.Fatal("integer precision lost")
	}
	var b map[string]json.RawMessage
	if err := json.Unmarshal(seen, &b); err != nil {
		t.Fatal(err)
	}
	if string(b["model"]) != `"flash"` {
		t.Fatal("alias not mapped")
	}
	var ms []map[string]json.RawMessage
	json.Unmarshal(b["messages"], &ms)
	for _, i := range []int{0, 1, 2} {
		var s string
		json.Unmarshal(ms[i]["content"], &s)
		if !strings.Contains(s, "[[OBF-B64/1 ") {
			t.Fatalf("message %d not transformed", i)
		}
	}
	var tool string
	json.Unmarshal(ms[3]["content"], &tool)
	if tool != "<user_input>tool result</user_input>" {
		t.Fatal("tool result changed")
	}
	if !bytes.Contains(ms[2]["tool_calls"], []byte("args")) {
		t.Fatal("tool arguments changed")
	}
	var blocks []map[string]json.RawMessage
	json.Unmarshal(ms[4]["content"], &blocks)
	var text string
	json.Unmarshal(blocks[0]["text"], &text)
	if !strings.HasPrefix(text, "outside <user_input>[[OBF-B64/1 ") {
		t.Fatal("text block wrong")
	}
	if !bytes.Contains(blocks[1]["image_url"], []byte("leave")) {
		t.Fatal("non-text block changed")
	}
	if w.Body.String() != `{"model":"flash","choices":[],"usage":{"total_tokens":7}}` {
		t.Fatal("response rewritten")
	}
}

func TestAuthModelSchemaAndSizeFailuresNeverForward(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer up.Close()
	cfg := testConfig(up.URL)
	cfg.MaxInputBytes = 256
	h, err := newProxy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		body, token string
		code        int
	}{
		{`{"model":"混淆-flash","messages":[]}`, "wrong", 401},
		{`{"model":"not-allowed","messages":[]}`, "inbound-test-only", 400},
		{`{"model":"混淆-flash","messages":"bad"}`, "inbound-test-only", 400},
		{`{"model":"混淆-flash","messages":[{"role":"user","content":7}]}`, "inbound-test-only", 400},
		{`{"model":"混淆-flash","messages":[]} {}`, "inbound-test-only", 400},
		{strings.Repeat("x", 257), "inbound-test-only", 413},
	}
	for _, tt := range cases {
		if w := call(h, tt.body, tt.token); w.Code != tt.code {
			t.Errorf("status %d want %d", w.Code, tt.code)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid request reached upstream")
	}
}

func TestNoMatchAndUpstreamErrorsArePassedOnce(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		b, _ := io.ReadAll(r.Body)
		if !bytes.Contains(b, []byte(`"content":"unchanged"`)) {
			t.Error("plain input changed")
		}
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
		io.WriteString(w, `{"error":"quota"}`)
	}))
	defer up.Close()
	h, _ := newProxy(testConfig(up.URL))
	w := call(h, `{"model":"混淆-flash","messages":[{"role":"user","content":"unchanged"}]}`, "inbound-test-only")
	if w.Code != 429 || w.Body.String() != `{"error":"quota"}` || w.Header().Get("Retry-After") != "60" || calls.Load() != 1 || w.Header().Get("X-InputCheck-Transform") != "no_match" {
		t.Fatal("error/no-match semantics changed")
	}
}

func TestSSEArrivesBeforeUpstreamCompletes(t *testing.T) {
	release := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer up.Close()
	defer close(release)
	h, _ := newProxy(testConfig(up.URL))
	server := httptest.NewServer(h)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"混淆-flash","messages":[],"stream":true}`))
	r.Header.Set("Authorization", "Bearer inbound-test-only")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal("SSE buffered until completion:", err)
	}
	defer resp.Body.Close()
	buf := make([]byte, len("data: first\n\n"))
	if _, err := io.ReadFull(resp.Body, buf); err != nil || string(buf) != "data: first\n\n" {
		t.Fatal("first event missing", err)
	}
}

func TestClientCancellationReachesUpstream(t *testing.T) {
	canceled := make(chan struct{})
	started := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, "data: first\n\n")
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
		close(canceled)
	}))
	defer up.Close()
	h, _ := newProxy(testConfig(up.URL))
	server := httptest.NewServer(h)
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "POST", server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"混淆-flash","messages":[],"stream":true}`))
	r.Header.Set("Authorization", "Bearer inbound-test-only")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	cancel()
	resp.Body.Close()
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream request was not canceled")
	}
}

func TestConcurrencyLimitDoesNotQueue(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-release }))
	defer up.Close()
	defer close(release)
	cfg := testConfig(up.URL)
	cfg.MaxConcurrent = 1
	h, _ := newProxy(cfg)
	done := make(chan struct{})
	go func() { call(h, `{"model":"混淆-flash","messages":[]}`, "inbound-test-only"); close(done) }()
	<-started
	w := call(h, `{"model":"混淆-flash","messages":[]}`, "inbound-test-only")
	if w.Code != 503 {
		t.Fatalf("overload status %d", w.Code)
	}
}

func TestRejectUnsafeConfiguration(t *testing.T) {
	for _, url := range []string{"", "file:///tmp/test", "http://user:password@example.invalid", "https://example.invalid?key=secret", "http://example.invalid/other"} {
		if _, err := newProxy(testConfig(url)); err == nil {
			t.Fatalf("unsafe target accepted")
		}
	}
	c := testConfig("http://127.0.0.1:12345")
	c.Token = ""
	if _, err := newProxy(c); err == nil {
		t.Fatal("empty token")
	}
}

func TestOutputLimitsQueriesCompressionAndRedirects(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Location", "https://example.invalid/untrusted")
		w.WriteHeader(307)
	}))
	defer up.Close()
	cfg := testConfig(up.URL)
	cfg.MaxOutputBytes = 128
	h, _ := newProxy(cfg)
	body := `{"model":"混淆-flash","messages":[{"role":"user","content":"<user_input>hello</user_input>"}]}`
	if w := call(h, body, "inbound-test-only"); w.Code != 413 {
		t.Fatalf("expansion not bounded: %d", w.Code)
	}
	if calls.Load() != 0 {
		t.Fatal("oversized output forwarded")
	}
	cfg.MaxOutputBytes = 0
	h, _ = newProxy(cfg)
	for _, query := range []string{"?key=test-only-secret", "?"} {
		r := httptest.NewRequest("POST", "/v1/chat/completions"+query, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer inbound-test-only")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("query not rejected")
		}
	}
	r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer inbound-test-only")
	r.Header.Set("Content-Encoding", "gzip")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 || calls.Load() != 0 {
		t.Fatal("compressed body reached upstream")
	}
	w = call(h, body, "inbound-test-only")
	if w.Code != 502 || w.Header().Get("Location") != "" || calls.Load() != 1 {
		t.Fatal("redirect was followed or exposed")
	}
}

func BenchmarkChatRequest(b *testing.B) {
	tr, _ := codec.New(24)
	for _, n := range []int{100 << 10, 1 << 20} {
		raw, _ := json.Marshal(map[string]any{"model": "混淆-flash", "messages": []map[string]string{{"role": "user", "content": "<user_input>" + strings.Repeat("甲", n/3) + "</user_input>"}}})
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			b.SetBytes(int64(len(raw)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, _, err := transformRequest(raw, map[string]string{"混淆-flash": "flash"}, tr); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestGatewayDeadlineReturns504BeforeHeaders(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer up.Close()
	cfg := testConfig(up.URL)
	cfg.Timeout = time.Second
	h, err := newProxy(cfg)
	if err != nil {
		t.Fatal(err)
	}
	w := call(h, `{"model":"混淆-flash","messages":[]}`, "inbound-test-only")
	if w.Code != 504 || !strings.Contains(w.Body.String(), "upstream_timeout") {
		t.Fatalf("gateway timeout became %d %q", w.Code, w.Body.String())
	}
}

func TestStalledReaderReleasesHandlerAfterDeadline(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		block := strings.Repeat("x", 64<<10)
		for {
			if _, err := io.WriteString(w, block); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			default:
			}
		}
	}))
	defer up.Close()
	cfg := testConfig(up.URL)
	cfg.Timeout = time.Second
	cfg.MaxConcurrent = 1
	h, _ := newProxy(cfg)
	returned := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { defer close(returned); h.ServeHTTP(w, r) }))
	defer server.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	body := `{"model":"混淆-flash","messages":[]}`
	fmt.Fprintf(conn, "POST /v1/chat/completions HTTP/1.1\r\nHost: localhost\r\nAuthorization: Bearer inbound-test-only\r\nContent-Length: %d\r\n\r\n%s", len(body), body)
	// Deliberately never read from conn. The upstream context alone cannot unblock a socket write.
	select {
	case <-returned:
	case <-time.After(3 * time.Second):
		t.Fatal("stalled reader retained request beyond bounded write grace")
	}
}

func TestTimeoutStatusAndKeepAliveOverHTTP(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer up.Close()
	cfg := testConfig(up.URL)
	cfg.Timeout = time.Second
	h, _ := newProxy(cfg)
	server := httptest.NewServer(h)
	defer server.Close()
	transport := &http.Transport{MaxConnsPerHost: 1}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 4 * time.Second}
	req, _ := http.NewRequest("POST", server.URL+"/v1/chat/completions", strings.NewReader(`{"model":"混淆-flash","messages":[]}`))
	req.Header.Set("Authorization", "Bearer inbound-test-only")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 504 || !strings.Contains(string(body), "upstream_timeout") {
		t.Fatalf("wire status %d", resp.StatusCode)
	}
	// net/http must reset the previous response deadline for a reused connection.
	resp, err = client.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal("health request failed after timeout")
	}
}

func TestLocalErrorHasStableOriginMessage(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unauthorized request forwarded") }))
	defer up.Close()
	h, _ := newProxy(testConfig(up.URL))
	response := call(h, `{"model":"混淆-flash","messages":[]}`, "wrong")
	var body struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(response.Body.Bytes(), &body) != nil || response.Code != 401 || body.Error.Message != "inputcheck: unauthorized" || body.Error.Type != "input_check_error" || body.Error.Code != "unauthorized" {
		t.Fatal("gateway local error has no stable origin message")
	}
}

// Catch both gateway admission caps and hidden transport connection caps.
func TestUnlimitedConcurrencyAllowsSeventeenInflightRequests(t *testing.T) {
	started := make(chan struct{}, 17)
	release := make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()
	cfg := testConfig(up.URL)
	cfg.MaxConcurrent = -1
	h, err := newProxy(cfg)
	if err != nil {
		t.Fatalf("unlimited configuration rejected: %v", err)
	}
	done := make(chan int, 17)
	defer func() {
		close(release)
		for i := 0; i < 17; i++ {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("request did not finish")
			}
		}
	}()
	for i := 0; i < 17; i++ {
		go func() { done <- call(h, `{"model":"混淆-flash","messages":[]}`, "inbound-test-only").Code }()
	}
	for i := 0; i < 17; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("not all requests reached upstream concurrently")
		}
	}
}
