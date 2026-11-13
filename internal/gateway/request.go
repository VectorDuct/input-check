package gateway

import (
	"encoding/json"
	"errors"
	"inputcheck/internal/codec"
	"unicode/utf8"
)

var errRequest = errors.New("invalid chat request")
var errModel = errors.New("model is not allowed")

func transformRequest(raw []byte, models map[string]string, tr *codec.Transformer, limits ...int) ([]byte, codec.Stats, error) {
	var stats codec.Stats
	if !utf8.Valid(raw) {
		return nil, stats, errRequest
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil || root == nil {
		return nil, stats, errRequest
	}
	var model string
	if json.Unmarshal(root["model"], &model) != nil {
		return nil, stats, errRequest
	}
	target, ok := models[model]
	if !ok {
		return nil, stats, errModel
	}
	var messages []json.RawMessage
	if len(root["messages"]) == 0 || string(root["messages"]) == "null" || json.Unmarshal(root["messages"], &messages) != nil {
		return nil, stats, errRequest
	}
	limit := 32 << 20
	if len(limits) > 0 {
		limit = limits[0]
	}
	// Reserve the entire request, including untouched fields, before expanding text.
	baseline := rawJSONSize(raw)
	modelGrowth := jsonStringSize(target) - jsonStringSize(model)
	if modelGrowth > 0 {
		baseline += modelGrowth
	}
	if baseline > limit && target != model {
		return nil, stats, codec.ErrLimit
	}
	budget := &outputBudget{remaining: limit - baseline, limit: limit}
	changed := false
	for i, rawMessage := range messages {
		var message map[string]json.RawMessage
		if json.Unmarshal(rawMessage, &message) != nil || message == nil {
			return nil, stats, errRequest
		}
		var role string
		if json.Unmarshal(message["role"], &role) != nil {
			return nil, stats, errRequest
		}
		switch role {
		case "tool", "function":
			continue
		case "system", "developer", "user", "assistant":
		default:
			return nil, stats, errRequest
		}
		content := message["content"]
		if len(content) == 0 || string(content) == "null" {
			continue
		}
		replacement, did, part, err := transformContent(content, tr, budget)
		if err != nil {
			return nil, stats, err
		}
		stats.Matched += part.Matched
		stats.Encoded += part.Encoded
		stats.Skipped += part.Skipped
		stats.Ambiguous += part.Ambiguous
		stats.Partial += part.Partial
		if did {
			message["content"] = replacement
			messages[i], err = marshalBounded(message, limit)
			if err != nil {
				return nil, stats, errRequest
			}
			changed = true
		}
	}
	if changed {
		encoded, err := marshalBounded(messages, limit)
		if err != nil {
			return nil, stats, errRequest
		}
		root["messages"] = encoded
	}
	if target != model {
		root["model"], _ = json.Marshal(target)
		changed = true
	}
	if !changed {
		if len(raw) > limit {
			return nil, stats, codec.ErrLimit
		}
		return raw, stats, nil
	}
	result, err := marshalBounded(root, limit)
	if err != nil {
		return nil, stats, errRequest
	}
	return result, stats, nil
}

func transformContent(content json.RawMessage, tr *codec.Transformer, budget *outputBudget) (json.RawMessage, bool, codec.Stats, error) {
	var stats codec.Stats
	var text string
	if json.Unmarshal(content, &text) == nil {
		value, part, err := budget.text(tr, text)
		if err != nil {
			return nil, false, stats, err
		}
		if value == text {
			return content, false, part, nil
		}
		encoded, err := marshalBounded(value, budget.limit)
		return encoded, true, part, err
	}
	var blocks []json.RawMessage
	if json.Unmarshal(content, &blocks) != nil || blocks == nil {
		return nil, false, stats, errRequest
	}
	changed := false
	for i, rawBlock := range blocks {
		var block map[string]json.RawMessage
		if json.Unmarshal(rawBlock, &block) != nil || block == nil {
			return nil, false, stats, errRequest
		}
		var kind string
		if json.Unmarshal(block["type"], &kind) != nil {
			return nil, false, stats, errRequest
		}
		if kind != "text" {
			continue
		}
		if json.Unmarshal(block["text"], &text) != nil || string(block["text"]) == "null" {
			return nil, false, stats, errRequest
		}
		value, part, err := budget.text(tr, text)
		if err != nil {
			return nil, false, stats, err
		}
		stats.Matched += part.Matched
		stats.Encoded += part.Encoded
		stats.Skipped += part.Skipped
		stats.Ambiguous += part.Ambiguous
		stats.Partial += part.Partial
		if value != text {
			block["text"], _ = marshalBounded(value, budget.limit)
			blocks[i], err = marshalBounded(block, budget.limit)
			if err != nil {
				return nil, false, stats, errRequest
			}
			changed = true
		}
	}
	if !changed {
		return content, false, stats, nil
	}
	result, err := marshalBounded(blocks, budget.limit)
	return result, true, stats, err
}

// Encode prepares a checked request using an identity model mapping.
// Callers must run check.Checker first and reject warnings before calling it.
func Encode(raw []byte, tags []string, chunk, limit int) ([]byte, codec.Stats, error) {
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		return nil, codec.Stats{}, errRequest
	}
	var model string
	if json.Unmarshal(root["model"], &model) != nil || model == "" {
		return nil, codec.Stats{}, errRequest
	}
	tr, err := codec.NewWithTags(chunk, tags)
	if err != nil {
		return nil, codec.Stats{}, err
	}
	return transformRequest(raw, map[string]string{model: model}, tr, limit)
}
