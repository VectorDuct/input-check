package codec

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// Independent decoder checks the externally documented format, not private helpers.
func restoreBlocks(t *testing.T, value string) string {
	t.Helper()
	const start = "[[OBF-B64/1 "
	const finish = "[[/OBF-B64/1]]"
	for {
		at := strings.Index(value, start)
		if at < 0 {
			return value
		}
		end := strings.Index(value[at:], finish)
		if end < 0 {
			t.Fatal("missing envelope close")
		}
		end += at
		lines := strings.Split(value[at:end], "\n")
		var size int
		var digest string
		if _, err := fmt.Sscanf(lines[0], "[[OBF-B64/1 bytes=%d sha256=%s", &size, &digest); err != nil {
			t.Fatal(err)
		}
		var raw []byte
		seq := 1
		for _, line := range lines[2:] {
			if line == "" {
				continue
			}
			number, encoded, ok := strings.Cut(line, ":")
			if !ok {
				t.Fatal("bad chunk")
			}
			n, err := strconv.Atoi(number)
			if err != nil || n != seq {
				t.Fatal("chunk ordering")
			}
			part, err := base64.StdEncoding.Strict().DecodeString(encoded)
			if err != nil {
				t.Fatal(err)
			}
			raw = append(raw, part...)
			seq++
		}
		if len(raw) != size {
			t.Fatalf("length %d want %d", len(raw), size)
		}
		value = value[:at] + string(raw) + value[end+len(finish):]
	}
}

func TestClosedTagsRoundTripAndIdempotence(t *testing.T) {
	tr, err := New(24)
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"user_input", "user_Input", "用户输入", "USER_INPUT"} {
		original := "outside\n<" + tag + "> 甲\n\n乙🙂é\t </" + tag + ">\nuntouched"
		got, stats, err := tr.Text(original)
		if err != nil {
			t.Fatal(err)
		}
		if got == original || stats.Encoded != 1 {
			t.Fatalf("closed tag was not encoded: %+v", stats)
		}
		if restoreBlocks(t, got) != original {
			t.Fatal("whitespace or Unicode changed")
		}
		again, stats, err := tr.Text(got)
		if err != nil || again != got || stats.Encoded != 0 || stats.Skipped != 1 {
			t.Fatalf("not idempotent: %+v %v", stats, err)
		}
	}
}

func TestUnpairedAndOtherTagsStayUntouched(t *testing.T) {
	tr, _ := New(24)
	for _, s := range []string{"<user_input>unclosed", "closing</user_input>", "<userInput>x</userInput>", "<user_input></user_input>"} {
		got, _, err := tr.Text(s)
		if err != nil || got != s {
			t.Fatalf("unexpected change: %q %v", s, err)
		}
	}
}

func TestNestedAndIncompleteOuter(t *testing.T) {
	tr, _ := New(24)
	for _, s := range []string{"<user_input>a<user_input>b</user_input>c</user_input>", "<user_input>open <用户输入>inner</用户输入> tail", "<user_input>x</USER_INPUT>"} {
		got, stats, err := tr.Text(s)
		if err != nil || stats.Encoded != 1 || restoreBlocks(t, got) != s {
			t.Fatalf("pairing failed: %+v %v", stats, err)
		}
		twice, _, err := tr.Text(got)
		if err != nil || twice != got {
			t.Fatal("nested idempotence")
		}
	}
	crossed := "<user_input>a<用户输入>b</user_input>c</用户输入>"
	got, _, err := tr.Text(crossed)
	if err != nil || got != crossed {
		t.Fatal("crossing tags must not be encoded")
	}
}

func TestMixedPriorEnvelopeAndNewText(t *testing.T) {
	tr, _ := New(2)
	old, _, err := tr.Text("<user_input>甲乙</user_input>")
	if err != nil {
		t.Fatal(err)
	}
	payload := strings.TrimSuffix(strings.TrimPrefix(old, "<user_input>"), "</user_input>")
	input := "<user_input>新增\n" + payload + "\n尾部</user_input>"
	got, stats, err := tr.Text(input)
	if err != nil || stats.Encoded != 2 || stats.Skipped != 1 {
		t.Fatalf("mixed: %+v %v", stats, err)
	}
	if strings.Count(got, payload) != 1 {
		t.Fatal("prior envelope changed")
	}
	if restoreBlocks(t, got) != "<user_input>新增\n甲乙\n尾部</user_input>" {
		t.Fatal("mixed content lost")
	}
	again, _, err := tr.Text(got)
	if err != nil || again != got {
		t.Fatal("mixed idempotence")
	}
}

func TestMalformedEnvelopeIsNotTrusted(t *testing.T) {
	tr, _ := New(24)
	input := "<user_input>[[OBF-B64/1 bytes=999 sha256=bad]]\nDecode\n1:!!!!\n[[/OBF-B64/1]]</user_input>"
	got, stats, err := tr.Text(input)
	if err != nil || stats.Encoded != 1 || stats.Skipped != 0 || got == input {
		t.Fatalf("malformed marker accepted: %+v %v", stats, err)
	}
	// Do not recursively decode this intentionally invalid original marker.
}

func TestCodecChunkBoundaries(t *testing.T) {
	tr, _ := New(1)
	s := "<用户输入>甲🙂\n</用户输入>"
	got, _, err := tr.Text(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "1:55Sy\n2:8J+Zgg==\n3:Cg==\n") {
		t.Fatal("chunks split Unicode or reorder text")
	}
	if restoreBlocks(t, got) != s {
		t.Fatal("roundtrip")
	}
}

func TestBounds(t *testing.T) {
	if _, err := New(0); err == nil {
		t.Fatal("zero chunk size accepted")
	}
	if _, err := New(257); err == nil {
		t.Fatal("oversized chunk accepted")
	}
	tr, _ := New(24)
	if _, _, err := tr.Text(strings.Repeat("<user_input>", 65)); err == nil {
		t.Fatal("unbounded nesting accepted")
	}
	if _, _, err := tr.Text(string([]byte{0xff})); err == nil {
		t.Fatal("invalid UTF8 accepted")
	}
}

func BenchmarkText(b *testing.B) {
	tr, _ := New(24)
	for _, n := range []int{100 << 10, 1 << 20} {
		input := "<user_input>" + strings.Repeat("a", n) + "</user_input>"
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			b.SetBytes(int64(len(input)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, _, err := tr.Text(input); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestChecksumTamperingIsReencoded(t *testing.T) {
	tr, _ := New(24)
	encoded, _, err := tr.Text("<user_input>hello</user_input>")
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(encoded, "1:aGVsbG8=", "1:d29ybGQ=", 1)
	if tampered == encoded {
		t.Fatal("test fixture did not change")
	}
	got, stats, err := tr.Text(tampered)
	if err != nil || stats.Skipped != 0 || stats.Encoded != 1 || got == tampered {
		t.Fatalf("tampered envelope trusted: %+v %v", stats, err)
	}
}

func FuzzTextIdempotence(f *testing.F) {
	for _, s := range []string{"<user_input>甲\n\n乙🙂</user_input>", "<user_input>outer<用户输入>inner</用户输入>", "<user_input><用户输入>x</user_input></用户输入>", "plain", "<user_input>[[OBF-B64/1 invalid]]</user_input>"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 8192 {
			return
		}
		tr, _ := New(24)
		a, _, err := tr.Text(s)
		if err != nil {
			return
		}
		b, _, err := tr.Text(a)
		if err != nil || a != b {
			t.Fatal("second pass changed first result")
		}
	})
}

func BenchmarkMalformedMarkers(b *testing.B) {
	tr, _ := New(24)
	input := "<user_input>" + strings.Repeat("[[OBF-B64/1 invalid]]", 5000) + "</user_input>"
	b.SetBytes(int64(len(input)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, _, err := tr.Text(input); err != nil {
			b.Fatal(err)
		}
	}
}

func TestTextOutputBudget(t *testing.T) {
	tr, _ := New(1)
	input := "<user_input>" + strings.Repeat("x", 10000) + "</user_input>"
	if _, _, err := tr.TextLimit(input, 128); err != ErrLimit {
		t.Fatalf("expected bounded rejection, got %v", err)
	}
	small := "<user_input>甲\n乙</user_input>"
	encoded, _, err := tr.Text(small)
	if err != nil {
		t.Fatal(err)
	}
	if value, _, err := tr.TextLimit(small, len(encoded)); err != nil || value != encoded {
		t.Fatal("exact byte budget rejected")
	}
	if _, _, err := tr.TextLimit(small, len(encoded)-1); err != ErrLimit {
		t.Fatal("one-byte-over budget accepted")
	}
	if _, _, err := tr.TextLimit("untagged", 2); err != ErrLimit {
		t.Fatal("unmatched text bypassed budget")
	}
}
