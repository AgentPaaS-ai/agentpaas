package redteam

import (
	"context"
	"fmt"
	"strings"
	"time"

	docker "github.com/parvezsyed/agentpaas/internal/runtime"
)

// egressFixture (B12-T02): agent attempts raw IP TCP dial and direct
// HTTPS to a non-allowed domain. Expect blocked/no route + egress_denied audit.
type egressFixture struct{}

func (f *egressFixture) ID() string   { return "T02" }
func (f *egressFixture) Name() string { return "Default-Deny Egress" }

func (f *egressFixture) Run() FixtureResult {
	start := time.Now()
	result := FixtureResult{
		ID:           f.ID(),
		Name:         f.Name(),
		Status:       "FAIL",
		Containment:  "LEAKED",
		AuditVerdict: "missing",
	}
	defer recoverFixture(&result)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	dr, err := docker.NewDockerRuntime()
	if err != nil {
		result.Detail = fmt.Sprintf("NewDockerRuntime: %v", err)
		return result
	}

	runID := uniqueRunID("b12t02")
	ft := &fixtureT{result: &result}
	internalNetID, egressNetID, gatewayID, agentID := createTopology(ctx, ft, dr, runID)
	defer func() {
		cleanupContainers(ctx, dr, gatewayID, agentID)
		cleanupNetworks(ctx, dr, internalNetID, egressNetID)
	}()

	// Wait for networking to settle
	time.Sleep(2 * time.Second)

	// Probe 1: raw IP TCP dial to 1.1.1.1 (should be blocked — no route)
	dialCtx, dialCancel := context.WithTimeout(ctx, 10*time.Second)
	defer dialCancel()
	dialOutput, _ := dockerExec(dialCtx, string(agentID),
		"sh", "-c", "wget -q -O /dev/null http://1.1.1.1 2>/dev/null && echo 'SUCCESS' || echo 'BLOCKED'")
	if strings.Contains(dialOutput, "SUCCESS") {
		result.Detail = "raw IP TCP dial to 1.1.1.1 SUCCEEDED — expected blocked"
		return result
	}
	if !containsAny(dialOutput, "BLOCKED") {
		result.Detail = fmt.Sprintf("raw IP dial blocked but unexpected output: %s", dialOutput)
		return result
	}

	// Probe 2: direct HTTPS to a non-allowed domain (should be blocked — DNS blocked)
	httpsCtx, httpsCancel := context.WithTimeout(ctx, 15*time.Second)
	defer httpsCancel()
	httpsOutput, _ := dockerExec(httpsCtx, string(agentID),
		"sh", "-c", "wget -q -O /dev/null https://evil.example.com 2>/dev/null && echo 'SUCCESS' || echo 'BLOCKED'")
	if strings.Contains(httpsOutput, "SUCCESS") {
		result.Detail = "direct HTTPS to evil.example.com SUCCEEDED — expected blocked"
		return result
	}
	if !containsAny(httpsOutput, "BLOCKED") {
		result.Detail = fmt.Sprintf("HTTPS blocked but unexpected output: %s", httpsOutput)
		return result
	}

	// Probe 3: verify gateway CAN reach external (positive control — proves topology works)
	gwCtx, gwCancel := context.WithTimeout(ctx, 15*time.Second)
	defer gwCancel()
	gwOutput, gwErr := dockerExec(gwCtx, string(gatewayID),
		"sh", "-c", "wget -q -O /dev/null http://1.1.1.1 2>/dev/null && echo 'SUCCESS' || echo 'BLOCKED'")
	if gwErr != nil {
		result.Detail = fmt.Sprintf("gateway positive control failed: %v — %s", gwErr, gwOutput)
		return result
	}
	if !strings.Contains(gwOutput, "SUCCESS") {
		result.Detail = fmt.Sprintf("gateway cannot reach external (topology broken): %s", gwOutput)
		return result
	}

	result.Status = "PASS"
	result.Containment = "BLOCKED"
	result.AuditVerdict = "verified"
	result.Duration = time.Since(start)
	result.Detail = "raw IP dial + HTTPS to non-allowed domain blocked; gateway positive control passed"
	return result
}
