package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	connect3270 "github.com/3270io/3270Connect/connect3270"
	"github.com/3270io/3270Connect/internal/profiler"
)

// startPrometheusListener starts a dedicated HTTP server on addr that serves
// the Prometheus /metrics endpoint. Runs in a goroutine; failures are
// logged but do not abort the workflow runner.
func startPrometheusListener(addr string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Printf("Prometheus metrics listening on %s/metrics", addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Prometheus listener exited: %v", err)
		}
	}()
}

// runProfileMode is the one-shot probe path triggered by -profile. It
// connects, runs profiler.Probe, writes the resulting JSON to -profileOut
// (or stdout), and exits.
//
// The workflow runner is intentionally not invoked. Failures are logged and
// the process exits with a non-zero status so CI can fail fast.
func runProfileMode() {
	host := strings.TrimSpace(profileHost)
	port := profilePort

	if host == "" || port == 0 {
		// Fall back to the workflow config so callers can reuse their
		// existing workflow.json instead of duplicating connection info.
		if cfg, err := loadConfigurationSafe(configFile); err == nil && cfg != nil {
			if host == "" {
				host = cfg.Host
			}
			if port == 0 {
				port = cfg.Port
			}
		}
	}
	if host == "" || port == 0 {
		log.Fatalf("profile mode: host and port are required (use -profileHost / -profilePort or a workflow config)")
	}

	scriptPort := strings.TrimSpace(os.Getenv("PROFILE_SCRIPT_PORT"))
	if scriptPort == "" {
		scriptPort = "5050"
	}

	e := connect3270.NewEmulator(host, port, scriptPort)
	defer func() { _ = e.Disconnect() }()

	if err := e.Connect(); err != nil {
		log.Fatalf("profile mode: connect failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	p, err := profiler.Probe(ctx, e, profiler.ProbeOptions{
		Tool:       "3270Connect",
		Version:    version,
		Host:       host,
		Port:       port,
		TLS:        profileTLS,
		CollectRaw: profileCollectRaw,
	})
	if err != nil {
		log.Fatalf("profile mode: probe failed: %v", err)
	}

	out, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		log.Fatalf("profile mode: marshal failed: %v", err)
	}

	if strings.TrimSpace(profileOut) == "" {
		fmt.Println(string(out))
		return
	}
	if err := os.WriteFile(profileOut, append(out, '\n'), 0o644); err != nil {
		log.Fatalf("profile mode: write %s failed: %v", profileOut, err)
	}
}

// loadConfigurationSafe wraps loadConfiguration so callers can use it without
// triggering os.Exit on parse errors (loadConfiguration calls log.Fatal on
// failure, which is undesirable for the optional fallback in profile mode).
func loadConfigurationSafe(path string) (*Configuration, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("no configuration path provided")
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Configuration
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
