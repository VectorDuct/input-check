package gateway

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"inputcheck/internal/check"
	"inputcheck/internal/codec"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// New exposes an authenticated check endpoint and, only when configured, a fixed-upstream proxy.
// Inspection never edits content. Reversible encoding requires Mode="encode".
func New(c Config) (http.Handler, error) {
	if len(c.Token) < 16 || strings.ContainsAny(c.Token, "\r\n\t ") {
		return nil, errors.New("gateway token must be one token of at least 16 bytes")
	}
	if c.Mode == "" {
		c.Mode = "inspect"
	}
	if c.Mode != "inspect" && c.Mode != "encode" {
		return nil, errors.New("mode must be inspect or encode")
	}
	if c.Tags == nil {
		c.Tags = append([]string(nil), check.DefaultTags...)
	}
	checker, err := check.New(c.Tags)
	if err != nil {
		return nil, err
	}
	if c.MaxInputBytes == 0 {
		c.MaxInputBytes = 1 << 20
	}
	if c.MaxOutputBytes == 0 {
		c.MaxOutputBytes = 8 << 20
	}
	if c.MaxConcurrent == 0 {
		c.MaxConcurrent = 2
	}
	if c.Timeout == 0 {
		c.Timeout = 60 * time.Second
	}
	if c.MaxInputBytes < 1 || c.MaxInputBytes > 8<<20 || c.MaxOutputBytes < 1 || c.MaxOutputBytes > 32<<20 || c.MaxConcurrent < 1 || c.MaxConcurrent > 16 || c.Timeout < time.Second || c.Timeout > time.Hour {
		return nil, errors.New("invalid resource bounds")
	}
	var proxy http.Handler
	if c.Upstream != "" {
		proxy, err = newProxy(c)
		if err != nil {
			return nil, err
		}
	} else if c.Mode != "inspect" || c.UpstreamToken != "" || len(c.Models) > 0 {
		return nil, errors.New("proxy settings require an upstream")
	}
	slots := make(chan struct{}, c.MaxConcurrent)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Path == "/healthz" && r.Method == http.MethodGet && r.URL.RawQuery == "" && !r.URL.ForceQuery {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"status":"ready"}`)
			return
		}
		isCheck := r.URL.Path == "/v1/check"
		if !isCheck && (r.URL.Path != "/v1/chat/completions" || proxy == nil) {
			writeError(w, 404, "not_found")
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeError(w, 405, "method_not_allowed")
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+c.Token)) != 1 {
			writeError(w, 401, "unauthorized")
			return
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery || r.Header.Get("Content-Encoding") != "" && r.Header.Get("Content-Encoding") != "identity" {
			writeError(w, 400, "unsupported_request_encoding_or_query")
			return
		}
		media := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
		if media != "application/json" {
			writeError(w, 415, "application_json_required")
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			w.Header().Set("Retry-After", "1")
			writeError(w, 503, "gateway_busy")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), c.Timeout)
		defer cancel()
		controller := http.NewResponseController(w)
		_ = controller.SetWriteDeadline(time.Now().Add(c.Timeout + time.Second))
		readTimeout := min(c.Timeout, 30*time.Second)
		_ = controller.SetReadDeadline(time.Now().Add(readTimeout))
		raw, readErr := io.ReadAll(io.LimitReader(r.Body, c.MaxInputBytes+1))
		_ = controller.SetReadDeadline(time.Time{})
		if readErr != nil {
			writeError(w, 400, "invalid_body")
			return
		}
		if int64(len(raw)) > c.MaxInputBytes {
			writeError(w, 413, "input_too_large")
			return
		}
		if ctx.Err() != nil {
			writeError(w, 408, "request_timeout")
			return
		}
		report := checker.Inspect(raw)
		status := http.StatusOK
		if !report.Valid {
			status = 400
		} else if report.Warnings > 0 && (c.RejectWarnings || !isCheck && c.Mode == "encode") {
			status = 422
		}
		w.Header().Set("X-InputCheck-Mode", "inspect")
		w.Header().Set("X-InputCheck-Warnings", strconv.Itoa(report.Warnings))
		if isCheck || status != 200 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(report)
			return
		}
		w.Header().Set("X-InputCheck-Mode", c.Mode)
		req := r.Clone(ctx)
		req.Body = io.NopCloser(bytes.NewReader(raw))
		req.ContentLength = int64(len(raw))
		req.GetBody = nil
		proxy.ServeHTTP(w, req)
	}), nil
}

func inspectRequest(raw []byte, models map[string]string, limit int) ([]byte, error) {
	var root map[string]json.RawMessage
	if json.Unmarshal(raw, &root) != nil {
		return nil, errRequest
	}
	var model string
	if json.Unmarshal(root["model"], &model) != nil {
		return nil, errRequest
	}
	target, ok := models[model]
	if !ok {
		return nil, errModel
	}
	if target == model {
		if len(raw) > limit {
			return nil, codec.ErrLimit
		}
		return raw, nil
	}
	root["model"], _ = json.Marshal(target)
	return marshalBounded(root, limit)
}
