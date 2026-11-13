// Input Check is a local input inspector with an optional fixed-upstream gateway.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"inputcheck/internal/check"
	"inputcheck/internal/gateway"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const version = "0.1.0"

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(stdout, "Input Check\nCommands: check, encode, serve, healthcheck, version\nUse <command> -h for options.")
		return 0
	}
	if args[0] == "version" || args[0] == "-version" {
		fmt.Fprintln(stdout, version)
		return 0
	}
	command := args[0]
	if command != "check" && command != "encode" && command != "serve" && command != "healthcheck" {
		fmt.Fprintln(stderr, "unknown command")
		return 2
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	config := fs.String("config", "", "JSON configuration file")
	var input *string
	var tags *string
	var strict *bool
	var maximum *int64
	var validate *bool
	if command == "check" || command == "encode" {
		input = fs.String("input", "-", "request JSON file, or - for stdin")
		tags = fs.String("tags", "", "comma-separated literal tag names (override config)")
		strict = fs.Bool("strict", false, "return exit 1 on warnings; encode always requires no warnings")
		maximum = fs.Int64("max-input-bytes", 0, "input byte limit (override config, 1..8388608)")
	}
	if command == "serve" {
		validate = fs.Bool("check-config", false, "validate settings without reading credentials or making requests")
	}
	if err := fs.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected positional arguments")
		return 2
	}
	cfg, err := loadConfig(*config)
	if err != nil {
		fmt.Fprintln(stderr, "invalid configuration:", err)
		return 2
	}
	if command == "serve" {
		if *validate {
			fmt.Fprintln(stdout, "Configuration valid; credentials and upstream were not checked.")
			return 0
		}
		if err := serve(cfg, stdout); err != nil {
			fmt.Fprintln(stderr, "server error:", err)
			return 2
		}
		return 0
	}
	if command == "healthcheck" {
		if healthcheck(cfg) != nil {
			fmt.Fprintln(stderr, "local healthcheck failed")
			return 1
		}
		fmt.Fprintln(stdout, "ready")
		return 0
	}
	if *tags != "" {
		cfg.Tags = strings.Split(*tags, ",")
		for i := range cfg.Tags {
			cfg.Tags[i] = strings.TrimSpace(cfg.Tags[i])
		}
	}
	if *maximum != 0 {
		cfg.MaxInputBytes = *maximum
	}
	if cfg.MaxInputBytes < 1 || cfg.MaxInputBytes > 8<<20 {
		fmt.Fprintln(stderr, "invalid input byte limit")
		return 2
	}
	inspector, err := check.New(cfg.Tags)
	if err != nil {
		fmt.Fprintln(stderr, "invalid tag configuration")
		return 2
	}
	reader := stdin
	if *input != "-" {
		f, err := os.Open(*input)
		if err != nil {
			fmt.Fprintln(stderr, "cannot read input file")
			return 2
		}
		defer f.Close()
		reader = f
	}
	raw, err := io.ReadAll(io.LimitReader(reader, cfg.MaxInputBytes+1))
	if err != nil {
		fmt.Fprintln(stderr, "cannot read input")
		return 2
	}
	report := inspector.Inspect(raw)
	if int64(len(raw)) > cfg.MaxInputBytes {
		report = check.Report{Valid: false, InputBytes: len(raw), Findings: []check.Finding{{Code: "input_too_large", Severity: "error", MessageIndex: -1, BlockIndex: -1, Offset: -1}}}
	}
	status := 0
	if !report.Valid {
		status = 2
	} else if report.Warnings > 0 && (*strict || cfg.RejectWarnings || command == "encode") {
		status = 1
	}
	if command == "check" {
		json.NewEncoder(stdout).Encode(report)
		return status
	}
	if status != 0 {
		json.NewEncoder(stderr).Encode(report)
		return status
	}
	output, _, err := gateway.Encode(raw, cfg.Tags, cfg.ChunkRunes, int(cfg.MaxOutputBytes))
	if err != nil {
		fmt.Fprintln(stderr, "encoding failed: output budget or unsupported request")
		return 2
	}
	if _, err = stdout.Write(output); err != nil {
		return 2
	}
	fmt.Fprintln(stdout)
	return 0
}

func serve(cfg fileConfig, output io.Writer) error {
	c := cfg.gatewayConfig()
	var err error
	c.Token, err = readCredential(cfg.GatewayTokenFile, cfg.GatewayTokenEnv)
	if err != nil {
		return err
	}
	if cfg.Upstream != "" {
		c.UpstreamToken, err = readCredential(cfg.UpstreamTokenFile, cfg.UpstreamTokenEnv)
		if err != nil {
			return err
		}
	}
	c.Logger = slog.New(slog.NewJSONHandler(output, nil))
	handler, err := gateway.New(c)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: cfg.Listen, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32 << 10, ErrorLog: log.New(io.Discard, "", 0)}
	stop, cleanup := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cleanup()
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	fmt.Fprintln(output, "Input Check", version, "starting")
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return errors.New("HTTP service could not start")
	case <-stop.Done():
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.ShutdownSeconds)*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			c.Logger.Warn("waiting_for_active_requests")
			if server.Shutdown(context.Background()) != nil {
				return errors.New("graceful shutdown failed")
			}
		}
		err := <-done
		if !errors.Is(err, http.ErrServerClosed) {
			return errors.New("HTTP service stopped unexpectedly")
		}
		return nil
	}
}

func healthcheck(cfg fileConfig) error {
	host, port, _ := net.SplitHostPort(cfg.Listen)
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return errors.New("not ready")
	}
	return nil
}

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }
