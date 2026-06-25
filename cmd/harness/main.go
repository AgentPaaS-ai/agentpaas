package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/parvezsyed/agentpaas/internal/harness"
)

func main() {
	cfg := harness.Config{
		Addr:           envOrDefault("AGENTPAAS_ADDR", "127.0.0.1:8080"),
		AgentPath:      envOrDefault("AGENTPAAS_AGENT_PATH", "/agent/main.py"),
		Python:         detectPython(),
		ImportTimeout:  envDuration("AGENTPAAS_IMPORT_TIMEOUT", 60*time.Second),
		InvokeTimeout:  envDuration("AGENTPAAS_INVOKE_TIMEOUT", 300*time.Second),
		TerminateGrace: envDuration("AGENTPAAS_TERMINATE_GRACE", 10*time.Second),
		StdoutPath:     envOrDefault("AGENTPAAS_STDOUT_PATH", "/dev/stdout"),
		StderrPath:     envOrDefault("AGENTPAAS_STDERR_PATH", "/dev/stderr"),
	}

	// Wire the audit appender if a path is provided. The daemon mounts
	// a volume and sets AGENTPAAS_AUDIT_PATH so harness audit events
	// (egress decisions, MCP calls) flow back to the daemon.
	if auditPath := os.Getenv("AGENTPAAS_AUDIT_PATH"); auditPath != "" {
		appender, err := harness.NewFileAuditAppender(auditPath)
		if err != nil {
			log.Printf("harness: audit appender: %v", err)
		} else {
			cfg.Audit = appender
			defer func() { _ = appender.Close() }()
		}
	}

	server := harness.NewServer(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigCh
		log.Printf("harness: received signal %v, shutting down", sig)
		cancel()
	}()

	log.Printf("harness: listening on %s, agent=%s", cfg.Addr, cfg.AgentPath)
	if err := server.ListenAndServe(ctx); err != nil {
		log.Printf("harness: server error: %v", err)
		os.Exit(1)
	}
}

func detectPython() string {
	if v := os.Getenv("AGENTPAAS_PYTHON"); v != "" {
		return v
	}
	for _, name := range []string{"python3", "python"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return "python3"
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func envDuration(key string, defaultVal time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return defaultVal
}