package codec

import (
	"strings"
	"testing"
)

func TestInputNameVariants(t *testing.T) {
	tr, _ := New(24)
	for _, name := range []string{"玩家输入", "user输入", "USER输入", "player_input", "本轮玩家输入", "本轮用户输入", "本轮user输入", "当前用户输入", "原始用户输入", "本回合输入"} {
		original := "before<" + name + ">甲\n\n乙🙂</" + name + ">after"
		got, stats, err := tr.Text(original)
		if err != nil || stats.Encoded != 1 || restoreBlocks(t, got) != original {
			t.Fatalf("closed name %s: %+v %v", name, stats, err)
		}
		again, _, err := tr.Text(got)
		if err != nil || again != got {
			t.Fatalf("closed name not idempotent: %s", name)
		}
	}
	for _, s := range []string{"<玩家输入>甲</用户输入>", "<user输入>甲", "<玩家动作>甲</玩家动作>", "<userInput>x</userInput>"} {
		got, _, err := tr.Text(s)
		if err != nil || got != s {
			t.Fatal("unpaired/mismatched/unrecognized name changed")
		}
	}
}

func TestInputHeadingParagraphBoundaries(t *testing.T) {
	tr, _ := New(24)
	for _, name := range []string{"本轮玩家输入", "本轮用户输入", "本轮user输入", "用户输入", "玩家输入", "user_input", "原始用户输入", "本回合输入"} {
		for _, nl := range []string{"\n", "\r\n"} {
			prefix := "【当前输入事实校准｜防记忆污染】" + nl + nl + "【" + name + "】" + nl
			payload := "虚构测试：甲抵达城门。" + nl + nl + "接着询问守卫。🙂"
			suffix := nl + "【Planner】" + nl + `{"g":"keep me unchanged"}` + nl
			input := prefix + payload + suffix
			got, stats, err := tr.Text(input)
			if err != nil || stats.Encoded != 1 || !strings.HasPrefix(got, prefix+envelopeStart) || !strings.HasSuffix(got, suffix) {
				t.Fatalf("heading %s: %+v %v", name, stats, err)
			}
			if restoreBlocks(t, got) != input {
				t.Fatal("multiline paragraph lost characters")
			}
			again, second, err := tr.Text(got)
			if err != nil || again != got || second.Encoded != 0 || second.Skipped != 1 {
				t.Fatalf("heading not idempotent: %+v %v", second, err)
			}
		}
	}
}

func TestHeadingEndOfMessageAndConsecutiveSections(t *testing.T) {
	tr, _ := New(24)
	for _, s := range []string{"【本轮玩家输入】\n甲\n\n乙", "【本轮玩家输入】\n甲\n\n乙\n", "【本轮玩家输入】\n甲\n【本轮用户输入】\n乙\n【Planner】\nremain", "  【本轮玩家输入】  \n甲\n  【Planner】 \nremain"} {
		got, stats, err := tr.Text(s)
		if err != nil || stats.Encoded < 1 || restoreBlocks(t, got) != s {
			t.Fatalf("heading boundary: %+v %v", stats, err)
		}
		again, _, err := tr.Text(got)
		if err != nil || again != got {
			t.Fatal("EOF/consecutive heading not idempotent")
		}
	}
	for _, s := range []string{"【本轮玩家输入】\n【Planner】\nremain", "规则引用【本轮玩家输入】不得省略", "【本轮用户输入】中描述的行动必须展开", "【本轮最终执行稿】\n任务说明", "【当前输入事实校准｜防记忆污染】\n\n【Planner】\nremain"} {
		got, _, err := tr.Text(s)
		if err != nil || got != s {
			t.Fatal("empty section or inline instruction changed")
		}
	}
}

func TestClosedAndHeadingRangesOverlap(t *testing.T) {
	tr, _ := New(24)
	fixtures := []string{
		"【本轮玩家输入】\n前缀<玩家输入>甲</玩家输入>后缀\n【Planner】\nremain",
		"<玩家输入>\n【本轮用户输入】\n甲\n【Planner】\nkeep inside outer tag</玩家输入>",
		"<玩家输入>\n【本轮用户输入】\n甲</玩家输入>\noutside\n【Planner】\nremain",
	}
	for _, input := range fixtures {
		got, stats, err := tr.Text(input)
		if err != nil || stats.Encoded != 1 || restoreBlocks(t, got) != input {
			t.Fatalf("overlap: %+v %v", stats, err)
		}
		again, _, err := tr.Text(got)
		if err != nil || again != got {
			t.Fatal("overlap not idempotent")
		}
	}
}

func TestHeadingMixedPreviousEnvelopeAndNewText(t *testing.T) {
	tr, _ := New(24)
	old, _, err := tr.Text("【本轮玩家输入】\n原有内容\n【Planner】\nunchanged")
	if err != nil {
		t.Fatal(err)
	}
	mixed := strings.Replace(old, "\n【Planner】", "\n新增第二段\n\n第三段\n【Planner】", 1)
	got, stats, err := tr.Text(mixed)
	if err != nil || stats.Skipped != 1 || stats.Encoded != 1 {
		t.Fatalf("mixed heading: %+v %v", stats, err)
	}
	if !strings.HasSuffix(got, "\n【Planner】\nunchanged") || restoreBlocks(t, got) != "【本轮玩家输入】\n原有内容\n新增第二段\n\n第三段\n【Planner】\nunchanged" {
		t.Fatal("mixed heading boundary or content changed")
	}
	again, _, err := tr.Text(got)
	if err != nil || again != got {
		t.Fatal("mixed heading not idempotent")
	}
}

func TestHeadingCountLimit(t *testing.T) {
	tr, _ := New(24)
	if _, _, err := tr.Text(strings.Repeat("【Planner】\n", maxTags+1)); err != ErrLimit {
		t.Fatal("unbounded heading count accepted")
	}
}

func TestHeadingDoesNotUseInternalClosedBlockBoundary(t *testing.T) {
	tr, _ := New(24)
	for _, input := range []string{
		"【本轮用户输入】\noutside\n<玩家输入>\n【Planner】\ninside\n</玩家输入>\ntail",
		"【本轮玩家输入】\ntext\n【note <玩家输入>inside</玩家输入>】\nmore\n【Planner】\nremain",
	} {
		got, _, err := tr.Text(input)
		if err != nil || restoreBlocks(t, got) != input {
			t.Fatal("overlap roundtrip failed")
		}
		again, stats, err := tr.Text(got)
		if err != nil || again != got || stats.Encoded != 0 {
			t.Fatal("hidden closed-block heading changed second-pass range")
		}
	}
}

func TestCrossedAngleBlocksCannotBeRepairedByHeadingEncoding(t *testing.T) {
	tr, _ := New(24)
	input := "<玩家输入>\n【本轮用户输入】\n<用户输入>\n【Planner】\n</玩家输入></用户输入>"
	first, _, err := tr.Text(input)
	if err != nil {
		t.Fatal(err)
	}
	second, stats, err := tr.Text(first)
	if err != nil || first != second || stats.Encoded != 0 {
		t.Fatal("heading hid a crossing tag and changed second-pass pairing")
	}
}

func TestMixedStructuralIdempotenceMatrix(t *testing.T) {
	tr, _ := New(24)
	tokens := []string{"<玩家输入>", "</玩家输入>", "<用户输入>", "</用户输入>", "【本轮用户输入】", "【Planner】"}
	// Small deterministic structural matrix, not a fuzz/benchmark run.
	var visit func(string, int)
	visit = func(prefix string, left int) {
		if left > 0 {
			for _, token := range tokens {
				visit(prefix+token+"\n", left-1)
			}
			return
		}
		first, _, err := tr.Text(prefix)
		if err != nil {
			t.Fatal(err)
		}
		second, _, err := tr.Text(first)
		if err != nil || second != first {
			t.Fatalf("structural matrix changed on pass two: %q", prefix)
		}
		if restoreBlocks(t, first) != prefix {
			t.Fatal("structural matrix lost input")
		}
	}
	visit("", 5)
}
