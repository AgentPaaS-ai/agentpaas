package pack

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// VerificationResult holds the outcome of each post-build verification check.
type VerificationResult struct {
	SDKPresent     bool
	HarnessInImage bool
	EntryInImage   bool
	SDKInImage     bool
	HarnessFresh   bool
	HealthzOK      bool
	ReadyzOK       bool
}

// VerifyBuildOutput runs post-build verification on a built agent image.
// It checks that all required components are present and functional.
// Returns nil if all checks pass, or an error describing the first failure.
func VerifyBuildOutput(ctx context.Context, imageRef string, cfg BuildConfig) error {
	result := &VerificationResult{}

	// Check 1: SDK presence check — verify cfg.SDKDir was resolved at pack time.
	if cfg.SDKDir == "" {
		log.Printf("verify: SDK present = false")
		return fmt.Errorf("SDK directory was not resolved at pack time; agent image will be missing agentpaas_sdk")
	}
	result.SDKPresent = true
	log.Printf("verify: SDK present = true")

	// Check 2: Image content audit — verify harness, entry, and SDK are in the image.
	entryFile := "main.py"
	agentYAML, err := LoadAgentYAML(cfg.ProjectDir)
	if err == nil && agentYAML != nil && agentYAML.Entry != "" {
		entryFile = agentYAML.Entry
	}

	auditScript := fmt.Sprintf(
		`import os,json;print(json.dumps({"harness":os.path.isfile('/agentpaas/harness')and os.access('/agentpaas/harness',os.X_OK),"entry":os.path.isfile('/app/%s'),"sdk":os.path.isfile('/app/python/agentpaas_sdk/__init__.py')}))`,
		entryFile,
	)

	auditCtx, auditCancel := context.WithTimeout(ctx, 30*time.Second)
	defer auditCancel()
	auditCmd := exec.CommandContext(auditCtx, "docker", "run", "--rm", "--entrypoint", "/usr/bin/python3.11", imageRef, "-c", auditScript)
	auditOut, auditErr := auditCmd.Output()
	if auditErr != nil {
		return fmt.Errorf("image content audit: docker run failed: %w", auditErr)
	}

	var auditResult struct {
		Harness bool `json:"harness"`
		Entry   bool `json:"entry"`
		SDK     bool `json:"sdk"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(auditOut), &auditResult); err != nil {
		return fmt.Errorf("image content audit: parse JSON output: %w", err)
	}
	result.HarnessInImage = auditResult.Harness
	result.EntryInImage = auditResult.Entry
	result.SDKInImage = auditResult.SDK
	log.Printf("verify: harness in image = %v, entry in image = %v, SDK in image = %v",
		auditResult.Harness, auditResult.Entry, auditResult.SDK)

	if !auditResult.Harness {
		return fmt.Errorf("harness binary (/agentpaas/harness) not found or not executable in image")
	}
	if !auditResult.Entry {
		return fmt.Errorf("entry file (/app/%s) not found in image", entryFile)
	}
	if !auditResult.SDK {
		return fmt.Errorf("SDK (/app/python/agentpaas_sdk/__init__.py) not found in image")
	}

	// Check 3: Harness freshness check — compare MD5 of host harness vs image harness.
	hostMD5, err := computeFileMD5(cfg.HarnessPath)
	if err != nil {
		return fmt.Errorf("harness freshness: compute host harness MD5: %w", err)
	}

	md5Ctx, md5Cancel := context.WithTimeout(ctx, 30*time.Second)
	defer md5Cancel()
	md5Cmd := exec.CommandContext(md5Ctx, "docker", "run", "--rm", "--entrypoint", "/usr/bin/python3.11", imageRef, "-c",
		"import hashlib;print(hashlib.md5(open('/agentpaas/harness','rb').read()).hexdigest())")
	imageMD5Bytes, md5Err := md5Cmd.Output()
	if md5Err != nil {
		return fmt.Errorf("harness freshness: get image harness MD5: %w", md5Err)
	}
	imageMD5 := strings.TrimSpace(string(imageMD5Bytes))

	result.HarnessFresh = hostMD5 == imageMD5
	log.Printf("verify: harness fresh = %v (host=%s image=%s)", result.HarnessFresh, hostMD5, imageMD5)

	if !result.HarnessFresh {
		return fmt.Errorf("harness binary in image does not match host harness (stale embedded binary)")
	}

	// Check 4: Smoke test — start the container and poll healthz/readyz endpoints.
	smokeCtx, smokeCancel := context.WithTimeout(ctx, 15*time.Second)
	defer smokeCancel()

	runCmd := exec.CommandContext(ctx, "docker", "run", "-d", "--entrypoint", "/agentpaas/harness",
		"-e", "AGENTPAAS_AGENT_PATH=/app/"+entryFile, imageRef)
	runOut, runErr := runCmd.Output()
	if runErr != nil {
		return fmt.Errorf("smoke test: start container: %w", runErr)
	}
	containerID := strings.TrimSpace(string(runOut))
	defer func() {
		rmCtx, rmCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer rmCancel()
		_ = exec.CommandContext(rmCtx, "docker", "rm", "-f", containerID).Run() // best-effort external cleanup
	}()

	// Poll healthz endpoint until 200 or timeout.
	healthzOK := false
	healthzDeadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(healthzDeadline) {
		select {
		case <-smokeCtx.Done():
			return fmt.Errorf("smoke test: cancelled: %w", smokeCtx.Err())
		default:
		}

		healthzScript := "import urllib.request,urllib.error,sys\n" +
			"try:\n" +
			"  r=urllib.request.urlopen('http://127.0.0.1:8080/healthz',timeout=5)\n" +
			"  print(r.status,flush=True)\n" +
			"except urllib.error.HTTPError as e:\n" +
			"  print(f'ERROR {e.code}: {e.read().decode()}',flush=True)\n" +
			"  sys.exit(1)\n" +
			"except Exception as e:\n" +
			"  print(f'ERROR: {e}',flush=True)\n" +
			"  sys.exit(1)"

		healthCmd := exec.CommandContext(smokeCtx, "docker", "exec", containerID, "/usr/bin/python3.11", "-c", healthzScript)
		healthOut, healthErr := healthCmd.Output()
		if healthErr == nil {
			statusStr := strings.TrimSpace(string(healthOut))
			if statusStr == "200" {
				healthzOK = true
				result.HealthzOK = true
				log.Printf("verify: healthz = 200")
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	if !healthzOK {
		log.Printf("verify: healthz = false")
		return fmt.Errorf("smoke test: healthz endpoint did not return 200 within 15s; harness may not be functional")
	}

	// Poll readyz endpoint once (harness should be ready after healthz passes).
	readyzScript := "import urllib.request,urllib.error,sys\n" +
		"try:\n" +
		"  r=urllib.request.urlopen('http://127.0.0.1:8080/readyz',timeout=5)\n" +
		"  print(r.status,flush=True)\n" +
		"except urllib.error.HTTPError as e:\n" +
		"  body=e.read().decode()\n" +
		"  print(f'ERROR {e.code}: {body}',flush=True)\n" +
		"  sys.exit(1)\n" +
		"except Exception as e:\n" +
		"  print(f'ERROR: {e}',flush=True)\n" +
		"  sys.exit(1)"

	readyCmd := exec.CommandContext(smokeCtx, "docker", "exec", containerID, "/usr/bin/python3.11", "-c", readyzScript)
	readyOut, readyErr := readyCmd.Output()
	if readyErr != nil {
		output := strings.TrimSpace(string(readyOut))
		result.ReadyzOK = false
		log.Printf("verify: readyz = false")
		if strings.HasPrefix(output, "ERROR") {
			// Extract the error body (contains import traceback).
			parts := strings.SplitN(output, ":", 2)
			if len(parts) == 2 {
				body := strings.TrimSpace(parts[1])
				return fmt.Errorf("smoke test: readyz failed: %s", body)
			}
		}
		return fmt.Errorf("smoke test: readyz endpoint did not return 200: %s", output)
	}

	readyzStatus := strings.TrimSpace(string(readyOut))
	if readyzStatus == "200" {
		result.ReadyzOK = true
		log.Printf("verify: readyz = 200")
	} else {
		result.ReadyzOK = false
		log.Printf("verify: readyz = false")
		return fmt.Errorf("smoke test: readyz returned %s, expected 200", readyzStatus)
	}

	log.Printf("verify: all checks passed")
	return nil
}

// computeFileMD5 computes the MD5 hex digest of a file.
func computeFileMD5(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }() // best-effort close

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}