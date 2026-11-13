package codec

import (
	"strings"
	"testing"
)

func contentTransformer(t *testing.T) *Transformer {
	t.Helper()
	tr, err := New(24)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestContentClosedTag(t *testing.T) {
	tr := contentTransformer(t)
	for _, name := range []string{"content", "Content", "CONTENT"} {
		in := "<" + name + ">SENSITIVE_CONTENT_MARKER</" + name + ">"
		out, stats, err := tr.Text(in)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if stats.Matched != 1 || stats.Encoded != 1 || stats.Ambiguous != 0 || stats.Partial != 0 {
			t.Fatalf("%s: %+v", name, stats)
		}
		if strings.Contains(out, "SENSITIVE_CONTENT_MARKER") || !strings.Contains(out, envelopeStart) {
			t.Fatalf("%s: plaintext leaked or no envelope", name)
		}
		again, second, err := tr.Text(out)
		if err != nil || again != out || second.Encoded != 0 {
			t.Fatalf("%s: not idempotent: %v %+v", name, err, second)
		}
	}
}

func TestContentDoesNotMatchOtherTagNames(t *testing.T) {
	tr := contentTransformer(t)
	for _, in := range []string{
		"<container>SENSITIVE_CONTENT_MARKER</container>",
		"<content_block>SENSITIVE_CONTENT_MARKER</content_block>",
		"<my_content>SENSITIVE_CONTENT_MARKER</my_content>",
		"plain text with the word content and no tags",
	} {
		out, stats, err := tr.Text(in)
		if err != nil || out != in || stats.Encoded != 0 || stats.Matched != 0 {
			t.Fatalf("unexpected match for %q: %+v %v", in, stats, err)
		}
	}
}

func TestContentKeepsExistingTagsAndMarkers(t *testing.T) {
	tr := contentTransformer(t)
	names := []string{"历史前文", "剧情原文", "now_plot", "latest_input",
		"additional_information", "past_observe", "用户输入", "本轮用户输入",
		"user_input", "本轮输入"}
	for _, n := range names {
		in := "<" + n + ">LEGACY_MARKER</" + n + ">"
		out, stats, err := tr.Text(in)
		if err != nil || stats.Encoded != 1 || stats.Ambiguous != 0 || stats.Partial != 0 {
			t.Fatalf("%s: %+v %v", n, stats, err)
		}
		if strings.Contains(out, "LEGACY_MARKER") {
			t.Fatalf("%s: plaintext leaked", n)
		}
	}

	cases := []struct {
		name                                   string
		in                                     string
		wantAmbiguous, wantPartial, minEncoded int
	}{
		{"content_nested", "<content>AAA<now_plot>BBB</now_plot>CCC</content>", 0, 0, 1},
		{"content_stray_close", "<content>AAA</content>" + strings.Repeat("X", 10000) + "</content>", 1, 1, 1},
		{"content_unclosed_outer", "<content>" + strings.Repeat("X", 10000) + "<now_plot>BBB</now_plot>", 1, 1, 1},
		{"role_fragment", `<content>AAA "role": "system" }, { <now_plot>BBB</now_plot></content>`, 0, 0, 1},
	}
	for _, c := range cases {
		out, stats, err := tr.Text(c.in)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if stats.Ambiguous != c.wantAmbiguous || stats.Partial != c.wantPartial || stats.Encoded < c.minEncoded {
			t.Fatalf("%s: got %+v want amb=%d partial=%d minenc=%d", c.name, stats,
				c.wantAmbiguous, c.wantPartial, c.minEncoded)
		}
		again, second, err := tr.Text(out)
		if err != nil || again != out || second.Encoded != 0 {
			t.Fatalf("%s: not idempotent: %v %+v", c.name, err, second)
		}
	}
}
