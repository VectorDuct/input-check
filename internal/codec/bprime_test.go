package codec

import (
	"strings"
	"testing"
)

func bprimeTransformer(t *testing.T) *Transformer {
	t.Helper()
	tr, err := New(24)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestBPrimeNewTagNames(t *testing.T) {
	tr := bprimeTransformer(t)
	names := []string{"历史前文", "剧情原文", "now_plot", "latest_input", "additional_information", "past_observe"}
	for _, n := range names {
		in := "<" + n + ">SECRET_MARKER_" + n + "</" + n + ">"
		out, stats, err := tr.Text(in)
		if err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		if stats.Matched != 1 || stats.Encoded != 1 {
			t.Fatalf("%s: matched=%d encoded=%d", n, stats.Matched, stats.Encoded)
		}
		if stats.Ambiguous != 0 || stats.Partial != 0 {
			t.Fatalf("%s: unexpected ambiguity %+v", n, stats)
		}
		if strings.Contains(out, "SECRET_MARKER_") {
			t.Fatalf("%s: plaintext leaked", n)
		}
		if !strings.Contains(out, envelopeStart) {
			t.Fatalf("%s: no envelope produced", n)
		}
		again, second, err := tr.Text(out)
		if err != nil || again != out || second.Encoded != 0 {
			t.Fatalf("%s: not idempotent: %v %+v", n, err, second)
		}
		t.Logf("%s encoded=%d ambiguous=%d partial=%d", n, stats.Encoded, stats.Ambiguous, stats.Partial)
	}
	out, stats, err := tr.Text("<NOW_PLOT>SECRET_MARKER_UPPER</NOW_PLOT>")
	if err != nil || stats.Encoded != 1 || strings.Contains(out, "SECRET_MARKER_UPPER") {
		t.Fatalf("case-insensitive name failed: %v %+v", err, stats)
	}
}

func TestBPrimeLegacyNamesUnchanged(t *testing.T) {
	tr := bprimeTransformer(t)
	for _, n := range []string{"用户输入", "本轮用户输入", "玩家输入", "user_input", "本轮输入"} {
		in := "<" + n + ">LEGACY_MARKER</" + n + ">"
		out, stats, err := tr.Text(in)
		if err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		if stats.Encoded != 1 || stats.Ambiguous != 0 || stats.Partial != 0 {
			t.Fatalf("%s: %+v", n, stats)
		}
		if strings.Contains(out, "LEGACY_MARKER") {
			t.Fatalf("%s: plaintext leaked", n)
		}
	}
}

func TestBPrimeAmbiguityMarkers(t *testing.T) {
	tr := bprimeTransformer(t)
	long := strings.Repeat("X", 10000)
	cases := []struct {
		name                                   string
		in                                     string
		wantAmbiguous, wantPartial, minEncoded int
	}{
		{"nested", "<past_observe>AAA<now_plot>BBB</now_plot>CCC</past_observe>", 0, 0, 1},
		{"stray_close", "<past_observe>AAA</past_observe>" + long + "</past_observe>", 1, 1, 1},
		{"unclosed_outer", "<past_observe>" + long + "<now_plot>BBB</now_plot>", 1, 1, 1},
		{"role_fragment", `<past_observe>AAA "role": "system" }, { <now_plot>BBB</now_plot></past_observe>`, 0, 0, 1},
		{"crossed", "<past_observe>AAA<now_plot>BBB</past_observe>", 1, 0, 0},
	}
	for _, c := range cases {
		out, stats, err := tr.Text(c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if stats.Ambiguous != c.wantAmbiguous || stats.Partial != c.wantPartial || stats.Encoded < c.minEncoded {
			t.Fatalf("%s: got %+v want amb=%d partial=%d minenc=%d", c.name, stats, c.wantAmbiguous, c.wantPartial, c.minEncoded)
		}
		again, second, err := tr.Text(out)
		if err != nil || again != out || second.Encoded != 0 {
			t.Fatalf("%s: not idempotent: %v %+v", c.name, err, second)
		}
		t.Logf("%s bytes=%d->%d encoded=%d ambiguous=%d partial=%d", c.name, len(c.in), len(out), stats.Encoded, stats.Ambiguous, stats.Partial)
	}
}
