package gateway

import (
	"encoding/json"
	"errors"
	"inputcheck/internal/codec"
	"strings"
	"testing"
)

func TestJSONSizeMatchesEncoder(t *testing.T) {
	values := []string{"", "plain", "<user_input>甲\n\n乙🙂</user_input>", "\"\\\t\r\b\f\x00<&>\u2028\u2029", "\\\"<"}
	for _, value := range values {
		encoded, _ := json.Marshal(value)
		if jsonStringSize(value) != len(encoded) {
			t.Fatalf("string size: got %d want %d", jsonStringSize(value), len(encoded))
		}
	}
	raws := []string{` { "text" : "<>&甲" , "n" : 1 } `, `{"x":"\u003c\u2028\/\\\""}`, "{\"x\":\"\u2028\u2029\"}", `{"x":null,"a":[1,true,"a b"]}`}
	for _, raw := range raws {
		value := json.RawMessage(raw)
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if rawJSONSize(value) != len(encoded) {
			t.Fatalf("raw size: got %d want %d", rawJSONSize(value), len(encoded))
		}
		for _, shape := range []any{map[string]json.RawMessage{"<&>\u2028": value}, []json.RawMessage{value}} {
			want, _ := json.Marshal(shape)
			got, err := marshalBounded(shape, len(want))
			if err != nil || string(got) != string(want) {
				t.Fatal("exact JSON budget mismatch")
			}
			if _, err := marshalBounded(shape, len(want)-1); !errors.Is(err, codec.ErrLimit) {
				t.Fatal("oversized JSON allocated")
			}
		}
	}
}

func TestAggregateRequestOutputBudget(t *testing.T) {
	tr, _ := codec.New(1)
	models := map[string]string{"alias": "flash"}
	raw := []byte(`{"model":"alias","metadata":"<>&","messages":[{"role":"system","content":"<user_input>甲\n\n乙</user_input>"},{"role":"user","content":[{"type":"text","text":"<user_input>one</user_input>"},{"type":"text","text":"<user_input>two</user_input>"}]}]}`)
	output, stats, err := transformRequest(raw, models, tr)
	if err != nil || stats.Encoded != 3 {
		t.Fatalf("unbounded baseline: %v %v", stats, err)
	}
	// This fixture is already compact and canonical except HTML escaping, so exact size is accepted.
	again, _, err := transformRequest(raw, models, tr, len(output))
	if err != nil || string(again) != string(output) {
		t.Fatalf("exact aggregate budget rejected: %v", err)
	}
	if _, _, err := transformRequest(raw, models, tr, len(output)-1); !errors.Is(err, codec.ErrLimit) {
		t.Fatal("aggregate expansion escaped budget")
	}
	if _, _, err := transformRequest(raw, models, tr, len(raw)+250); !errors.Is(err, codec.ErrLimit) {
		t.Fatal("multiple text blocks bypassed budget")
	}
	huge, _ := json.Marshal(map[string]any{"model": "alias", "messages": []map[string]string{{"role": "user", "content": "<user_input>" + strings.Repeat("x", 10000) + "</user_input>"}}})
	if _, _, err := transformRequest(huge, models, tr, len(huge)+100); !errors.Is(err, codec.ErrLimit) {
		t.Fatal("small-chunk expansion accepted")
	}
}

func TestUnchangedIdentityRequestUsesOriginalByteBudget(t *testing.T) {
	tr, _ := codec.New(24)
	models := map[string]string{"flash": "flash"}
	// HTML escaping would exceed the limit, but this path forwards the original bytes.
	raw := []byte(`{"model":"flash","messages":[{"role":"user","content":"` + strings.Repeat("<", 1000) + `"}]}`)
	got, _, err := transformRequest(raw, models, tr, len(raw))
	if err != nil || string(got) != string(raw) {
		t.Fatalf("unchanged request rejected: %v", err)
	}
	if _, _, err := transformRequest(raw, models, tr, len(raw)-1); !errors.Is(err, codec.ErrLimit) {
		t.Fatal("original body escaped output limit")
	}
	changed := []byte(`{"model":"flash","messages":[{"role":"user","content":"` + strings.Repeat("<", 1000) + `<user_input>hello</user_input>"}]}`)
	if _, _, err := transformRequest(changed, models, tr, len(changed)); !errors.Is(err, codec.ErrLimit) {
		t.Fatal("changed identity body bypassed serialized budget")
	}
}
