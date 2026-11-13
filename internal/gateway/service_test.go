package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testInbound = "synthetic-client-token"

func checkCall(h http.Handler, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+testInbound)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestInspectionAPIWorksWithoutUpstreamAndNeverEchoesInput(t *testing.T) {
	h, err := New(Config{Token: testInbound})
	if err != nil {
		t.Fatal(err)
	}
	w := checkCall(h, "/v1/check", `{"model":"chat-a","messages":[{"role":"user","content":"SYNTHETIC_PRIVATE <input>x"}]}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "unclosed_tag") || strings.Contains(w.Body.String(), "SYNTHETIC_PRIVATE") {
		t.Fatalf("bad report: %d %s", w.Code, w.Body.String())
	}
	if w = checkCall(h, "/v1/chat/completions", `{}`); w.Code != 404 {
		t.Fatal("proxy route enabled without upstream")
	}
}
func TestInspectModePreservesExactRequestAndBlocksInvalidSchema(t *testing.T) {
	var seen string
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		b, _ := io.ReadAll(r.Body)
		seen = string(b)
		if r.Header.Get("Authorization") != "Bearer synthetic-upstream-token" {
			t.Error("wrong upstream auth")
		}
		io.WriteString(w, `{"choices":[]}`)
	}))
	defer up.Close()
	h, err := New(Config{Token: testInbound, UpstreamToken: "synthetic-upstream-token", Upstream: up.URL, Models: map[string]string{"chat-a": "chat-a"}})
	if err != nil {
		t.Fatal(err)
	}
	raw := "{ \"model\":\"chat-a\",\"messages\":[{\"role\":\"user\",\"content\":\"<input>hello</input>\"}],\"metadata\":{\"n\":900719925474099312345}}"
	w := checkCall(h, "/v1/chat/completions", raw)
	if w.Code != 200 || seen != raw || w.Header().Get("X-InputCheck-Mode") != "inspect" {
		t.Fatalf("inspection changed input: %d %s", w.Code, seen)
	}
	w = checkCall(h, "/v1/chat/completions", `{"model":"chat-a","messages":[{"role":"user","content":7}]}`)
	if w.Code != 400 || calls.Load() != 1 {
		t.Fatal("invalid schema reached upstream")
	}
}
func TestEncodeExplicitAndStrictWarningsPreventForward(t *testing.T) {
	var seen string
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		b, _ := io.ReadAll(r.Body)
		seen = string(b)
		io.WriteString(w, `{}`)
	}))
	defer up.Close()
	cfg := Config{Token: testInbound, UpstreamToken: "synthetic-upstream-token", Upstream: up.URL, Models: map[string]string{"chat-a": "chat-a"}, Mode: "encode"}
	h, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	w := checkCall(h, "/v1/chat/completions", `{"model":"chat-a","messages":[{"role":"user","content":"<input>hello</input>"}]}`)
	if w.Code != 200 || !strings.Contains(seen, "[[OBF-B64/1") || strings.Contains(seen, ">hello<") {
		t.Fatal("explicit encode not applied")
	}
	w = checkCall(h, "/v1/chat/completions", `{"model":"chat-a","messages":[{"role":"user","content":"<input>unclosed"}]}`)
	if w.Code != 422 || calls.Load() != 1 {
		t.Fatal("ambiguous encode reached upstream")
	}
}
func TestCheckEndpointAuthLimitsAndStrictPolicy(t *testing.T) {
	h, err := New(Config{Token: testInbound, RejectWarnings: true, MaxInputBytes: 200})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/v1/check", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("missing auth accepted")
	}
	w = checkCall(h, "/v1/check", `{"model":"a","messages":[{"role":"user","content":"<input>open"}]}`)
	if w.Code != 422 {
		t.Fatal("strict warning not rejected")
	}
	w = checkCall(h, "/v1/check", strings.Repeat("x", 201))
	if w.Code != 413 {
		t.Fatal("body limit ignored")
	}
}

func TestInspectOutputBudgetAndSharedRequestDeadline(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); io.WriteString(w, `{}`) }))
	defer up.Close()
	cfg := Config{Token: testInbound, UpstreamToken: "synthetic-upstream-token", Upstream: up.URL, Models: map[string]string{"chat-a": "chat-a"}, MaxOutputBytes: 10}
	h, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	w := checkCall(h, "/v1/chat/completions", `{"model":"chat-a","messages":[{"role":"user","content":"hello"}]}`)
	if w.Code != 413 || calls.Load() != 0 {
		t.Fatalf("output limit: %d", w.Code)
	}
}

type slowBody struct {
	reader *strings.Reader
	waited bool
}

func (s *slowBody) Read(p []byte) (int, error) {
	if !s.waited {
		time.Sleep(700 * time.Millisecond)
		s.waited = true
	}
	return s.reader.Read(p)
}
func TestOneDeadlineIncludesBodyReadAndUpstream(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(700 * time.Millisecond)
		io.WriteString(w, `{}`)
	}))
	defer up.Close()
	h, err := New(Config{Token: testInbound, UpstreamToken: "synthetic-upstream-token", Upstream: up.URL, Models: map[string]string{"chat-a": "chat-a"}, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/v1/chat/completions", &slowBody{reader: strings.NewReader(`{"model":"chat-a","messages":[{"role":"user","content":"hello"}]}`)})
	r.Header.Set("Authorization", "Bearer "+testInbound)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	start := time.Now()
	h.ServeHTTP(w, r)
	if w.Code != 504 || time.Since(start) > 1250*time.Millisecond {
		t.Fatalf("deadline reset between stages: status=%d elapsed=%v", w.Code, time.Since(start))
	}
}
