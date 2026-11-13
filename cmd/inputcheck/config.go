package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"inputcheck/internal/check"
	"inputcheck/internal/gateway"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type fileConfig struct {
	Listen            string            `json:"listen"`
	Mode              string            `json:"mode"`
	Upstream          string            `json:"upstream"`
	GatewayTokenFile  string            `json:"gateway_token_file"`
	GatewayTokenEnv   string            `json:"gateway_token_env"`
	UpstreamTokenFile string            `json:"upstream_token_file"`
	UpstreamTokenEnv  string            `json:"upstream_token_env"`
	Models            map[string]string `json:"models"`
	Tags              []string          `json:"tags"`
	ChunkRunes        int               `json:"chunk_runes"`
	RejectWarnings    bool              `json:"reject_warnings"`
	MaxInputBytes     int64             `json:"max_input_bytes"`
	MaxOutputBytes    int64             `json:"max_output_bytes"`
	MaxConcurrent     int               `json:"max_concurrent"`
	TimeoutSeconds    int               `json:"timeout_seconds"`
	ShutdownSeconds   int               `json:"shutdown_seconds"`
}

func loadConfig(path string) (fileConfig, error) {
	cfg := fileConfig{Listen: "127.0.0.1:9090", Mode: "inspect", Tags: append([]string(nil), check.DefaultTags...), ChunkRunes: 24,
		MaxInputBytes: 1 << 20, MaxOutputBytes: 8 << 20, MaxConcurrent: 2, TimeoutSeconds: 60, ShutdownSeconds: 90}
	if path != "" {
		f, err := os.Open(path)
		if err != nil {
			return cfg, errors.New("cannot read configuration")
		}
		defer f.Close()
		raw, err := io.ReadAll(io.LimitReader(f, 65537))
		if err != nil || len(raw) > 65536 {
			return cfg, errors.New("configuration is unreadable or too large")
		}
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || trimmed[0] != '{' || check.UniqueJSON(raw) != nil {
			return cfg, errors.New("configuration must be valid JSON with unique fields")
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if dec.Decode(&cfg) != nil {
			return cfg, errors.New("invalid or unknown configuration fields")
		}
	}
	host, port, err := net.SplitHostPort(cfg.Listen)
	if err != nil {
		return cfg, errors.New("invalid listen address")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 || host != "" && host != "localhost" && net.ParseIP(host) == nil {
		return cfg, errors.New("listen requires an IP address and valid port")
	}
	if cfg.TimeoutSeconds < 1 || cfg.TimeoutSeconds > 3600 || cfg.ShutdownSeconds < 1 || cfg.ShutdownSeconds > 7200 || cfg.ChunkRunes < 1 || cfg.ChunkRunes > 256 {
		return cfg, errors.New("invalid timeout or chunk size")
	}
	if cfg.MaxInputBytes < 1 || cfg.MaxInputBytes > 8<<20 || cfg.MaxOutputBytes < 1 || cfg.MaxOutputBytes > 32<<20 || cfg.MaxConcurrent < 1 || cfg.MaxConcurrent > 16 || len(cfg.Tags) == 0 {
		return cfg, errors.New("invalid resource or tag configuration")
	}
	for _, pair := range [][2]string{{cfg.GatewayTokenFile, cfg.GatewayTokenEnv}, {cfg.UpstreamTokenFile, cfg.UpstreamTokenEnv}} {
		if pair[0] != "" && pair[1] != "" {
			return cfg, errors.New("choose a secret file or environment variable, not both")
		}
		if pair[0] != "" && !filepath.IsAbs(pair[0]) {
			return cfg, errors.New("secret file paths must be absolute")
		}
		if pair[1] != "" && !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`).MatchString(pair[1]) {
			return cfg, errors.New("invalid credential environment variable name")
		}
	}
	if cfg.GatewayTokenFile == "" && cfg.GatewayTokenEnv == "" {
		cfg.GatewayTokenEnv = "INPUTCHECK_TOKEN"
	}
	if cfg.Upstream != "" && cfg.UpstreamTokenFile == "" && cfg.UpstreamTokenEnv == "" {
		cfg.UpstreamTokenEnv = "INPUTCHECK_UPSTREAM_TOKEN"
	}
	c := cfg.gatewayConfig()
	c.Token = "synthetic-config-client-token"
	if cfg.Upstream != "" {
		c.UpstreamToken = "synthetic-config-upstream-token"
	}
	if _, err := gateway.New(c); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c fileConfig) gatewayConfig() gateway.Config {
	return gateway.Config{Mode: c.Mode, Tags: c.Tags, RejectWarnings: c.RejectWarnings, Upstream: c.Upstream, Models: c.Models,
		ChunkRunes: c.ChunkRunes, MaxInputBytes: c.MaxInputBytes, MaxOutputBytes: c.MaxOutputBytes, MaxConcurrent: c.MaxConcurrent,
		Timeout: time.Duration(c.TimeoutSeconds) * time.Second}
}

func readCredential(file, env string) (string, error) {
	var raw []byte
	if file != "" {
		if !filepath.IsAbs(file) {
			return "", errors.New("secret file paths must be absolute")
		}
		f, err := os.Open(file)
		if err != nil {
			return "", errors.New("cannot read secret file")
		}
		defer f.Close()
		raw, err = io.ReadAll(io.LimitReader(f, 4097))
		if err != nil {
			return "", errors.New("cannot read secret file")
		}
	} else {
		raw = []byte(os.Getenv(env))
	}
	token := strings.TrimSpace(string(raw))
	if len(raw) > 4096 || len(token) < 16 || strings.ContainsAny(token, "\r\n\t ") {
		return "", errors.New("credential must be a single token of 16..4096 bytes")
	}
	return token, nil
}
