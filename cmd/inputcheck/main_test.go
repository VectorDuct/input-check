package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goodInput = `{"model":"chat-a","messages":[{"role":"user","content":"<input>PRIVATE_SYNTHETIC</input>"}]}`

func TestCheckCLIReportAndExitCodes(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		body   string
		status int
	}{
		{[]string{"check"}, goodInput, 0},
		{[]string{"check"}, `{"model":"a","messages":[{"role":"user","content":"<input>x"}]}`, 0},
		{[]string{"check", "-strict"}, `{"model":"a","messages":[{"role":"user","content":"<input>x"}]}`, 1},
		{[]string{"check"}, `{"model":"a","model":"b"}`, 2},
		{[]string{"check", "-max-input-bytes", "10"}, goodInput, 2},
	} {
		var out, errors bytes.Buffer
		status := run(tc.args, strings.NewReader(tc.body), &out, &errors)
		if status != tc.status {
			t.Fatalf("status %d want%d: %s", status, tc.status, errors.String())
		}
		if strings.Contains(out.String()+errors.String(), "PRIVATE_SYNTHETIC") {
			t.Fatal("checker leaked body")
		}
		var v map[string]any
		if json.Unmarshal(out.Bytes(), &v) != nil {
			t.Fatal("check report is not JSON")
		}
	}
}
func TestEncodeRequiresExplicitCommandAndCustomTags(t *testing.T) {
	var out, errors bytes.Buffer
	if run([]string{"encode"}, strings.NewReader(goodInput), &out, &errors) != 0 || !strings.Contains(out.String(), "[[OBF-B64/1") {
		t.Fatalf("encoding failed: %s", errors.String())
	}
	out.Reset()
	errors.Reset()
	raw := `{"model":"a","messages":[{"role":"user","content":"<note>value</note>"}]}`
	if run([]string{"encode", "-tags", "note"}, strings.NewReader(raw), &out, &errors) != 0 || !strings.Contains(out.String(), "[[OBF-B64/1") {
		t.Fatal("custom tag encoding failed")
	}
}
func TestEncodeRejectsAmbiguityWithEmptyStdout(t *testing.T) {
	var out, errors bytes.Buffer
	code := run([]string{"encode"}, strings.NewReader(`{"model":"a","messages":[{"role":"user","content":"<input>x"}]}`), &out, &errors)
	if code != 1 || out.Len() != 0 || !strings.Contains(errors.String(), "unclosed_tag") {
		t.Fatal("ambiguous encoding emitted a request")
	}
}
func TestConfigValidationDoesNotNeedSecretsOrEmitThem(t *testing.T) {
	file := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(file, []byte(`{"gateway_token_env":"MISSING_SYNTHETIC_TOKEN","tags":["note"]}`), 0600)
	var out, errors bytes.Buffer
	if run([]string{"serve", "-config", file, "-check-config"}, strings.NewReader(""), &out, &errors) != 0 {
		t.Fatal(errors.String())
	}
	for _, raw := range []string{`{"mode":"wrong"}`, `{"tags":["x","X"]}`, `{"listen":"x","listen":"y"}`, `{"unknown":true}`, `{"upstream":"https://user:pass@example.test"}`, `{"max_concurrent":-1}`, `{"max_concurrent":0}`, `{"max_input_bytes":0}`, `{"tags":null}`, `null`, `{"gateway_token_env":"BAD=NAME"}`} {
		os.WriteFile(file, []byte(raw), 0600)
		out.Reset()
		errors.Reset()
		if run([]string{"serve", "-config", file, "-check-config"}, strings.NewReader(""), &out, &errors) != 2 {
			t.Fatalf("invalid config accepted: %s", raw)
		}
	}
}
func TestInvalidSecretNeverAppearsInError(t *testing.T) {
	t.Setenv("INPUTCHECK_BAD_TEST_TOKEN", "SECRET_SYNTHETIC\nSECOND_LINE")
	_, err := readCredential("", "INPUTCHECK_BAD_TEST_TOKEN")
	if err == nil || strings.Contains(err.Error(), "SECRET_SYNTHETIC") {
		t.Fatal("unsafe credential diagnostic")
	}
}
