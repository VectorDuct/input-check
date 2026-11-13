package check

import (
	"encoding/json"
	"strings"
	"testing"
)

func request(text string) []byte {
	b, _ := json.Marshal(map[string]any{"model": "chat-a", "messages": []map[string]string{{"role": "user", "content": text}}})
	return b
}
func checker(t *testing.T) *Checker {
	t.Helper()
	c, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func has(r Report, code string) bool {
	for _, f := range r.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}

func TestInspectBalancedNestedTagsWithoutReturningText(t *testing.T) {
	raw := request("PRIVATE_SYNTHETIC <input>hello <context>世界</context></input>")
	report := checker(t).Inspect(raw)
	if !report.Valid || report.Warnings != 0 || report.PairedTags != 2 || report.TextBlocks != 1 {
		t.Fatalf("unexpected report: %+v", report)
	}
	out, _ := json.Marshal(report)
	if strings.Contains(string(out), "PRIVATE_SYNTHETIC") || strings.Contains(string(out), "hello") {
		t.Fatal("report echoed input")
	}
}
func TestStructuralAndUnicodeWarnings(t *testing.T) {
	for _, tc := range []struct{ text, code string }{
		{"<input>x", "unclosed_tag"}, {"</input>", "unmatched_close"},
		{"<input><context>x</input></context>", "crossed_tags"},
		{"hello\x00", "control_character"}, {"a\u202eb", "bidi_control"},
		{"<input>[[OBF-B64/1 bytes=2 sha256=bad]]oops</input>", "invalid_envelope"},
	} {
		r := checker(t).Inspect(request(tc.text))
		if !r.Valid || !has(r, tc.code) {
			t.Errorf("missing %s: %+v", tc.code, r)
		}
	}
}
func TestRejectDuplicateInvalidUTF8AndSchema(t *testing.T) {
	for _, raw := range [][]byte{
		[]byte(`{"model":"a","model":"b","messages":[]}`),
		[]byte(`{"model":"a","messages":[{"role":"user","content":"x","content":"y"}]}`),
		[]byte(`{"model":"a","messages":[{"role":"alien","content":"x"}]}`),
		[]byte(`{"model":"a","messages":[]}`),
		[]byte(`{"model":"a","messages":[{"role":"user","content":7}]}`),
		[]byte("{\"x\":\"\xff\"}"),
	} {
		r := checker(t).Inspect(raw)
		if r.Valid {
			t.Fatalf("accepted invalid document: %+v", r)
		}
	}
}
func TestToolsAndNonTextBlocksRemainOpaque(t *testing.T) {
	raw := []byte(`{"model":"a","messages":[{"role":"tool","content":"<input>unclosed"},{"role":"user","content":[{"type":"text","text":"【input】\nhello"},{"type":"image_url","image_url":{"url":"data:foo","note":"<input>unclosed"}}]}],"tools":[{"function":{"description":"<input>unclosed"}}]}`)
	r := checker(t).Inspect(raw)
	if !r.Valid || r.Warnings != 0 || r.TextBlocks != 1 || r.HeadingMarkers != 1 {
		t.Fatalf("opaque fields inspected: %+v", r)
	}
}
func TestCustomTagsAreLiteralCaseInsensitiveAndBounded(t *testing.T) {
	c, err := New([]string{"Customer_Note", "资料"})
	if err != nil {
		t.Fatal(err)
	}
	r := c.Inspect(request("<CUSTOMER_NOTE>x</customer_note><资料>y</资料><input>ignored"))
	if !r.Valid || r.Warnings != 0 || r.PairedTags != 2 {
		t.Fatalf("custom tags: %+v", r)
	}
	for _, tags := range [][]string{{"x", "X"}, {"a.*"}, {""}} {
		if _, err := New(tags); err == nil {
			t.Fatal("invalid tags accepted")
		}
	}
}
func TestDeepJSONAndExcessTagsAreBounded(t *testing.T) {
	r := checker(t).Inspect([]byte(strings.Repeat("[", 70) + "0" + strings.Repeat("]", 70)))
	if r.Valid || !has(r, "json_depth_limit") {
		t.Fatalf("depth not bounded: %+v", r)
	}
	r = checker(t).Inspect(request(strings.Repeat("<input>", 8193)))
	if r.Valid || !has(r, "structure_limit") {
		t.Fatalf("tag count not bounded: %+v", r)
	}
}
func TestWarningReportIsBounded(t *testing.T) {
	r := checker(t).Inspect(request(strings.Repeat("</input>", 300)))
	if !r.Valid || !r.Truncated || len(r.Findings) > 128 || r.Warnings != 300 {
		t.Fatalf("unbounded report: %+v", r)
	}
}

func TestUnicodeCaseFoldTagsAndLoneSurrogates(t *testing.T) {
	c, err := New([]string{"σ"})
	if err != nil {
		t.Fatal(err)
	}
	r := c.Inspect(request("<Σ>x</ς>"))
	if !r.Valid || r.Warnings != 0 || r.PairedTags != 1 {
		t.Fatalf("unicode casefold failed: %+v", r)
	}
	if _, err := New([]string{"σ", "ς"}); err == nil {
		t.Fatal("casefold duplicate tags accepted")
	}
	for _, raw := range []string{
		`{"model":"a","messages":[{"role":"user","content":"\ud800"}]}`,
		`{"model":"a","messages":[{"role":"user","content":"\udc00"}]}`,
	} {
		r := checker(t).Inspect([]byte(raw))
		if r.Valid || !has(r, "invalid_unicode_escape") {
			t.Fatalf("unpaired surrogate accepted: %+v", r)
		}
	}
	r = checker(t).Inspect([]byte(`{"model":"a","messages":[{"role":"user","content":"\ud83d\ude00 \\ud800"}]}`))
	if !r.Valid {
		t.Fatalf("valid surrogate pair or literal escape rejected: %+v", r)
	}
}
