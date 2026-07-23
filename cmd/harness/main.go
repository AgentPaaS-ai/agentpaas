package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/AgentPaaS-ai/agentpaas/internal/harness"
)

func main() {
	harness.InitEgressFirewall()
	harness.DropNetAdminCapability()

	cfg := harness.Config{
		Addr:            envOrDefault("AGENTPAAS_ADDR", "127.0.0.1:8080"),
		AgentPath:       envOrDefault("AGENTPAAS_AGENT_PATH", "/agent/main.py"),
		Python:          detectPython(),
		ImportTimeout: envDuration("AGENTPAAS_IMPORT_TIMEOUT", 60*time.Second), // legacy compat: import phase timeout (not a lifetime ceiling)
		// InvokeTimeout is the LEGACY v0.2.3 compat default for the /invoke
		// context timeout. On the durable path (B30-T03 Part B), the timeout
		// is derived from the TimeEnvelope carried in the invoke payload
		// (see Server.invokeTimeoutForPayload). AGENTPAAS_INVOKE_TIMEOUT
		// remains as a legacy compat override for the non-envelope path.
		InvokeTimeout:            envDuration("AGENTPAAS_INVOKE_TIMEOUT", 300*time.Second), // legacy compat: durable path uses invokeTimeoutForPayload
		TerminateGrace:           envDuration("AGENTPAAS_TERMINATE_GRACE", 10*time.Second), // legacy compat: SIGTERM grace (not a lifetime ceiling)
		StdoutPath:               envOrDefault("AGENTPAAS_STDOUT_PATH", "/dev/stdout"),
		StderrPath:               envOrDefault("AGENTPAAS_STDERR_PATH", "/dev/stderr"),
		CredentialsPath:          os.Getenv("AGENTPAAS_CREDENTIALS_PATH"),
		JournalKeyPath:           os.Getenv("AGENTPAAS_JOURNAL_KEY_PATH"),
		JournalPath:              os.Getenv("AGENTPAAS_JOURNAL_PATH"),
		AttemptID:                os.Getenv("AGENTPAAS_ATTEMPT_ID"),
		LeaseID:                  os.Getenv("AGENTPAAS_LEASE_ID"),
		RunID:                    os.Getenv("AGENTPAAS_RUN_ID"),
		DelegationSnapshotPath:   os.Getenv("AGENTPAAS_DELEGATION_SNAPSHOT_PATH"),
	}

	// B30-T04: durable-path resource ceilings. On the durable
	// (InvokeDeployment) path, the daemon sets AGENTPAAS_DURABLE_PATH=1 and
	// the policy-derived CPU/PID limits. On the legacy v0.2.3 path, these
	// env vars are absent and the Python runner falls back to the fixed
	// RLIMIT_CPU=30 / RLIMIT_NPROC=0 constants with "legacy compat"
	// comments.
	if v := os.Getenv("AGENTPAAS_DURABLE_PATH"); v == "1" || v == "true" {
		cfg.DurablePath = true
		cfg.CPUQuotaSeconds = envInt64("AGENTPAAS_CPU_QUOTA_SECONDS", 0)
		cfg.MaxPIDs = envInt("AGENTPAAS_MAX_PIDS", 0)
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
			defer func() { _ = appender.Close() }() // best-effort close
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

func envInt64(key string, defaultVal int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return defaultVal
}

func envInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultVal
}