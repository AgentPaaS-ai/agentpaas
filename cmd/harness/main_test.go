package main

import (
	"testing"
	"time"
)

func TestEnvOrDefault_ReturnsEnvValue(t *testing.T) {
	t.Setenv("AGENTPAAS_TEST_KEY", "custom-value")
	if got := envOrDefault("AGENTPAAS_TEST_KEY", "default"); got != "custom-value" {
		t.Fatalf("envOrDefault() = %q, want %q", got, "custom-value")
	}
}

func TestEnvOrDefault_ReturnsDefault(t *testing.T) {
	t.Setenv("AGENTPAAS_TEST_KEY", "")
	if got := envOrDefault("AGENTPAAS_TEST_KEY", "default"); got != "default" {
		t.Fatalf("envOrDefault() = %q, want %q", got, "default")
	}
}

func TestEnvDuration_ValidDuration(t *testing.T) {
	t.Setenv("AGENTPAAS_TEST_DURATION", "30s")
	if got := envDuration("AGENTPAAS_TEST_DURATION", time.Minute); got != 30*time.Second {
		t.Fatalf("envDuration() = %v, want %v", got, 30*time.Second)
	}
}

func TestEnvDuration_InvalidReturnsDefault(t *testing.T) {
	t.Setenv("AGENTPAAS_TEST_DURATION", "garbage")
	if got := envDuration("AGENTPAAS_TEST_DURATION", time.Minute); got != time.Minute {
		t.Fatalf("envDuration() = %v, want %v", got, time.Minute)
	}
}

func TestEnvDuration_MissingReturnsDefault(t *testing.T) {
	t.Setenv("AGENTPAAS_TEST_DURATION", "")
	if got := envDuration("AGENTPAAS_TEST_DURATION", time.Minute); got != time.Minute {
		t.Fatalf("envDuration() = %v, want %v", got, time.Minute)
	}
}