// Package gateway exposes an authenticated, fixed-upstream Chat Completions proxy.
package gateway

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"inputcheck/internal/codec"
)

type Config struct {
	Mode           string
	Tags           []string
	RejectWarnings bool
	Upstream       string
	Token          string
	UpstreamToken  string
	Models         map[string]string
	ChunkRunes     int
	MaxInputBytes  int64
	MaxOutputBytes int64
	MaxConcurrent  int
	Timeout        time.Duration
	Logger         *slog.Logger
}

func newProxy(c Config) (http.Handler, error) {
	target, err := url.Parse(c.Upstream)
	if err != nil || target == nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") || target.User != nil || target.RawQuery != "" || target.ForceQuery || target.Fragment != "" || (target.Path != "" && target.Path != "/") {
		return nil, errors.New("upstream must be an HTTP(S) origin without credentials, path, query or fragment")
	}
	if c.Token == "" || c.UpstreamToken == "" || strings.ContainsAny(c.Token+c.UpstreamToken, "\r\n\t ") || c.Token == c.UpstreamToken {
		return nil, errors.New("distinct nonempty gateway and upstream credentials are required")
	}
	if len(c.Models) == 0 || len(c.Models) > 32 {
		return nil, errors.New("model map must contain 1..32 entries")
	}
	models := make(map[string]string, len(c.Models))
	for alias, model := range c.Models {
		if alias == "" || model == "" || len(alias) > 256 || len(model) > 256 || strings.ContainsAny(alias+model, " \t\r\n?#") {
			return nil, errors.New("invalid model map")
		}
		models[alias] = model
	}
	if c.ChunkRunes == 0 {
		c.ChunkRunes = 24
	}
	var tr *codec.Transformer
	if c.Tags == nil {
		tr, err = codec.New(c.ChunkRunes)
	} else {
		tr, err = codec.NewWithTags(c.ChunkRunes, c.Tags)
	}
	if err != nil {
		return nil, err
	}
	if c.MaxInputBytes == 0 {
		c.MaxInputBytes = 8 << 20
	}
	if c.MaxOutputBytes == 0 {
		c.MaxOutputBytes = 32 << 20
	}
	if c.MaxConcurrent == 0 {
		c.MaxConcurrent = 2
	}
	if c.Timeout == 0 {
		c.Timeout = 15 * time.Minute
	}
	if c.MaxInputBytes < 1 || c.MaxInputBytes > 8<<20 || c.MaxOutputBytes < 1 || c.MaxOutputBytes > 32<<20 || c.MaxConcurrent < -1 || c.MaxConcurrent > 16 || c.Timeout < time.Second || c.Timeout > time.Hour {
		return nil, errors.New("invalid resource bounds")
	}
	if c.Logger == nil {
		c.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	// -1 removes admission and active upstream connection caps; zero retains the default.
	activeConnections, idleConnections := c.MaxConcurrent, c.MaxConcurrent
	if c.MaxConcurrent == -1 {
		activeConnections, idleConnections = 0, 64
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext, ForceAttemptHTTP2: true, MaxIdleConns: idleConnections, MaxIdleConnsPerHost: idleConnections, MaxConnsPerHost: activeConnections, IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: c.Timeout, DisableCompression: true}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(p *httputil.ProxyRequest) {
			p.SetURL(target)
			p.Out.URL.Path = "/v1/chat/completions"
			p.Out.URL.RawPath = ""
			p.Out.URL.RawQuery = ""
			p.Out.URL.ForceQuery = false
			p.Out.Host = target.Host
			p.Out.Header = make(http.Header)
			p.Out.Header.Set("Authorization", "Bearer "+c.UpstreamToken)
			p.Out.Header.Set("Content-Type", "application/json")
			p.Out.Header.Set("User-Agent", "inputcheck/0.1")
			p.Out.GetBody = nil
		},
		Transport: transport, FlushInterval: -1, ErrorLog: log.New(io.Discard, "", 0),
		ModifyResponse: func(r *http.Response) error {
			// A redirect must never cause a caller to resubmit a prompt outside the fixed upstream.
			if r.StatusCode >= 300 && r.StatusCode < 400 {
				r.Body.Close()
				return errors.New("upstream redirect rejected")
			}
			r.Header.Del("Set-Cookie")
			r.Header.Del("X-InputCheck-Mode")
			r.Header.Del("X-InputCheck-Warnings")
			r.Header.Del("X-InputCheck-Transform")
			r.Header.Del("X-InputCheck-Encoded")
			r.Header.Del("X-InputCheck-Ambiguous")
			r.Header.Del("X-InputCheck-Partial")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if errors.Is(r.Context().Err(), context.Canceled) {
				return
			}
			var timeout net.Error
			if errors.Is(r.Context().Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &timeout) && timeout.Timeout() {
				writeError(w, 504, "upstream_timeout")
				return
			}
			writeError(w, 502, "upstream_unavailable")
		},
	}
	var slots chan struct{}
	if c.MaxConcurrent > 0 {
		slots = make(chan struct{}, c.MaxConcurrent)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A context deadline does not interrupt a blocked downstream socket write.
		// Keep a bounded grace period to send a 504 if upstream headers time out.
		controller := http.NewResponseController(w)
		writeDeadline := time.Now().Add(c.Timeout + time.Second)
		if deadline, ok := r.Context().Deadline(); ok && deadline.Add(time.Second).Before(writeDeadline) {
			writeDeadline = deadline.Add(time.Second)
		}
		_ = controller.SetWriteDeadline(writeDeadline)
		if r.URL.Path == "/healthz" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"status":"ready"}`)
			return
		}
		if r.URL.Path != "/v1/chat/completions" {
			writeError(w, 404, "not_found")
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeError(w, 405, "method_not_allowed")
			return
		}
		expected := "Bearer " + c.Token
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte(expected)) != 1 {
			writeError(w, 401, "unauthorized")
			return
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery || r.Header.Get("Content-Encoding") != "" && r.Header.Get("Content-Encoding") != "identity" {
			writeError(w, 400, "unsupported_request_encoding_or_query")
			return
		}
		media := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
		if media != "" && media != "application/json" {
			writeError(w, 415, "application_json_required")
			return
		}
		if slots != nil {
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			default:
				w.Header().Set("Retry-After", "1")
				writeError(w, 503, "gateway_busy")
				return
			}
		}
		started := time.Now()
		ctx, cancel := context.WithTimeout(r.Context(), c.Timeout)
		defer cancel()
		deadline, _ := ctx.Deadline()
		if bodyDeadline := time.Now().Add(30 * time.Second); bodyDeadline.Before(deadline) {
			deadline = bodyDeadline
		}
		_ = controller.SetReadDeadline(deadline)
		raw, err := io.ReadAll(io.LimitReader(r.Body, c.MaxInputBytes+1))
		_ = controller.SetReadDeadline(time.Time{})
		if err != nil {
			var timeout net.Error
			if errors.As(err, &timeout) && timeout.Timeout() {
				writeError(w, 408, "request_timeout")
				return
			}
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
		var output []byte
		var stats codec.Stats
		if c.Mode == "inspect" {
			output, err = inspectRequest(raw, models, int(c.MaxOutputBytes))
		} else {
			output, stats, err = transformRequest(raw, models, tr, int(c.MaxOutputBytes))
		}
		if err != nil {
			if errors.Is(err, codec.ErrLimit) {
				writeError(w, 413, "transformation_limit")
			} else {
				writeError(w, 400, "invalid_request_or_model")
			}
			return
		}
		if int64(len(output)) > c.MaxOutputBytes {
			writeError(w, 413, "output_too_large")
			return
		}
		status := "no_match"
		if stats.Matched > 0 {
			status = "matched"
		}
		if stats.Encoded > 0 {
			status = "encoded"
		} else if stats.Skipped > 0 {
			status = "already_encoded"
		}
		w.Header().Set("X-InputCheck-Transform", status)
		w.Header().Set("X-InputCheck-Encoded", strconv.Itoa(stats.Encoded))
		w.Header().Set("X-InputCheck-Ambiguous", strconv.Itoa(stats.Ambiguous))
		w.Header().Set("X-InputCheck-Partial", strconv.Itoa(stats.Partial))
		processing := time.Since(started)
		req := r.Clone(ctx)
		req.Body = io.NopCloser(bytes.NewReader(output))
		req.ContentLength = int64(len(output))
		req.GetBody = nil
		req.TransferEncoding = nil
		proxy.ServeHTTP(w, req)
		c.Logger.Info("request_complete", "processing_us", processing.Microseconds(), "total_ms", time.Since(started).Milliseconds(), "input_bytes", len(raw), "output_bytes", len(output), "matched", stats.Matched, "encoded", stats.Encoded, "skipped", stats.Skipped, "ambiguous", stats.Ambiguous, "partial", stats.Partial, "status", status)
	}), nil
}

func writeError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"type": "input_check_error", "code": code, "message": "inputcheck: " + code}})
}
