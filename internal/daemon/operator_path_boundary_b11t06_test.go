package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	controlv1 "github.com/parvezsyed/agentpaas/api/control/v1"
	"github.com/parvezsyed/agentpaas/internal/operator"
	"google.golang.org/grpc/status"
)

func TestOperatorPathBoundaryRejectsOutsideProjectRoot(t *testing.T) {
	passwdBefore := operatorFileSnapshot(t, "/etc/passwd")
	server := newOperatorTestServer(t)

	resp, err := server.ValidateAgentProject(context.Background(), &controlv1.ValidateAgentProjectRequest{
		ProjectPath: "/etc",
	})
	if err != nil {
		assertOperatorProtoJSON(t, status.Convert(err).Proto())
		if !operatorErrorMentionsPathRestriction(err.Error()) {
			t.Fatalf("error does not identify path restriction: %v", err)
		}
	} else {
		assertValidationRefusal(t, resp)
		if !validationMentionsPathRestriction(resp) {
			t.Fatalf("validation refusal does not identify path restriction: %+v", resp.GetIssues())
		}
	}
	operatorAssertFileUnchanged(t, "/etc/passwd", passwdBefore)
}

func TestOperatorPathBoundaryRejectsSymlinkEscape(t *testing.T) {
	passwdBefore := operatorFileSnapshot(t, "/etc/passwd")
	projectParent := t.TempDir()
	projectLink := filepath.Join(projectParent, "escaped-project")
	if err := os.Symlink("/etc/passwd", projectLink); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	resp, err := (&stubControlServer{}).ValidateAgentProject(
		context.Background(),
		&controlv1.ValidateAgentProjectRequest{ProjectPath: projectLink},
	)
	if err != nil {
		t.Fatalf("ValidateAgentProject: %v", err)
	}
	assertValidationRefusal(t, resp)
	if !strings.Contains(strings.ToLower(validationIssueText(resp)), "symlink") {
		t.Fatalf("validation refusal does not identify symlink: %+v", resp.GetIssues())
	}
	operatorAssertFileUnchanged(t, "/etc/passwd", passwdBefore)
}

func TestOperatorPathBoundaryRejectsNullByte(t *testing.T) {
	passwdBefore := operatorFileSnapshot(t, "/etc/passwd")

	resp, err := (&stubControlServer{}).ValidateAgentProject(
		context.Background(),
		&controlv1.ValidateAgentProjectRequest{ProjectPath: "/tmp/project\x00/etc/passwd"},
	)
	if err != nil {
		t.Fatalf("ValidateAgentProject: %v", err)
	}
	assertValidationRefusal(t, resp)
	if !strings.Contains(strings.ToLower(validationIssueText(resp)), "null byte") {
		t.Fatalf("validation refusal does not identify null byte: %+v", resp.GetIssues())
	}
	operatorAssertFileUnchanged(t, "/etc/passwd", passwdBefore)
}

func TestOperatorPathBoundaryRejectsUnicodeTraversal(t *testing.T) {
	projectPath := filepath.Join(t.TempDir(), "\uff0e\uff0e", "escaped")

	resp, err := (&stubControlServer{}).ValidateAgentProject(
		context.Background(),
		&controlv1.ValidateAgentProjectRequest{ProjectPath: projectPath},
	)
	if err != nil {
		t.Fatalf("ValidateAgentProject: %v", err)
	}
	assertValidationRefusal(t, resp)
	issueText := strings.ToLower(validationIssueText(resp))
	if !strings.Contains(issueText, "non-ascii") && !strings.Contains(issueText, "traversal") {
		t.Fatalf("validation refusal does not identify unicode traversal: %+v", resp.GetIssues())
	}
}

func TestOperatorPathBoundaryRejectsAbsoluteAgentEntry(t *testing.T) {
	passwdBefore := operatorFileSnapshot(t, "/etc/passwd")
	projectDir := t.TempDir()
	writeOperatorTestFile(t, projectDir, "agent.yaml", `version: "1"
runtime: python
name: path-boundary-test
entry: /etc/passwd
`)
	writeOperatorTestFile(t, projectDir, "policy.yaml", validDefaultDenyPolicy)

	resp, err := (&stubControlServer{}).ValidateAgentProject(
		context.Background(),
		&controlv1.ValidateAgentProjectRequest{ProjectPath: projectDir},
	)
	if err != nil {
		t.Fatalf("ValidateAgentProject: %v", err)
	}
	assertValidationRefusal(t, resp)
	issueText := strings.ToLower(validationIssueText(resp))
	if !strings.Contains(issueText, "entry") || !strings.Contains(issueText, "absolute") {
		t.Fatalf("validation refusal does not identify absolute agent entry: %+v", resp.GetIssues())
	}
	operatorAssertFileUnchanged(t, "/etc/passwd", passwdBefore)
}

func TestOperatorPathBoundaryDoesNotResolveAuditPayloadPath(t *testing.T) {
	shadowBefore := operatorFileSnapshot(t, "/etc/shadow")
	const injectedPath = "../../../etc/shadow"
	server := newOperatorTestServer(t, operatorTestRecord("invoke", "run-path-payload", map[string]interface{}{
		"file_ref": injectedPath,
	}))
	before, err := server.auditIndex.RecordCount()
	if err != nil {
		t.Fatalf("RecordCount before GetRunTimeline: %v", err)
	}

	resp, err := server.GetRunTimeline(context.Background(), &controlv1.GetRunTimelineRequest{
		RunId: "run-path-payload",
	})
	if err != nil {
		t.Fatalf("GetRunTimeline: %v", err)
	}
	assertOperatorProtoJSON(t, resp)
	if len(resp.GetEvents()) != 1 {
		t.Fatalf("len(Events) = %d, want 1", len(resp.GetEvents()))
	}
	assertRedactedInjection(t, resp.GetEvents()[0].GetDescription(), injectedPath)
	after, err := server.auditIndex.RecordCount()
	if err != nil {
		t.Fatalf("RecordCount after GetRunTimeline: %v", err)
	}
	if after != before {
		t.Fatalf("RecordCount changed from %d to %d", before, after)
	}
	operatorAssertFileUnchanged(t, "/etc/shadow", shadowBefore)
}

type operatorFileState struct {
	exists  bool
	mode    os.FileMode
	size    int64
	modTime int64
}

func operatorFileSnapshot(t *testing.T, path string) operatorFileState {
	t.Helper()
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return operatorFileState{}
	}
	if err != nil {
		t.Fatalf("Lstat %s: %v", path, err)
	}
	return operatorFileState{
		exists:  true,
		mode:    info.Mode(),
		size:    info.Size(),
		modTime: info.ModTime().UnixNano(),
	}
}

func operatorAssertFileUnchanged(t *testing.T, path string, before operatorFileState) {
	t.Helper()
	after := operatorFileSnapshot(t, path)
	if after != before {
		t.Fatalf("outside file %s changed: before=%+v after=%+v", path, before, after)
	}
}

func assertValidationRefusal(t *testing.T, resp *controlv1.ValidateAgentProjectResponse) {
	t.Helper()
	assertOperatorProtoJSON(t, resp)
	if resp.GetReady() || resp.GetValid() {
		t.Fatalf("validation accepted malicious project path: %+v", resp)
	}
	if resp.GetSchemaVersion() == "" {
		t.Fatal("SchemaVersion is empty")
	}
	if len(resp.GetIssues()) == 0 {
		t.Fatal("validation refusal has no machine-readable issue")
	}
	if resp.GetIssues()[0].GetCategory() == "" {
		t.Fatal("validation refusal issue category is empty")
	}
	if resp.GetIssues()[0].GetNextAction() != string(operator.ActionFixCode) {
		t.Fatalf("NextAction = %q, want %q", resp.GetIssues()[0].GetNextAction(), operator.ActionFixCode)
	}
}

func validationIssueText(resp *controlv1.ValidateAgentProjectResponse) string {
	var text strings.Builder
	for _, issue := range resp.GetIssues() {
		text.WriteString(issue.GetMessage())
		text.WriteByte(' ')
	}
	for _, validation := range resp.GetValidations() {
		text.WriteString(validation.GetDetails())
		text.WriteByte(' ')
	}
	return text.String()
}

func validationMentionsPathRestriction(resp *controlv1.ValidateAgentProjectResponse) bool {
	return operatorErrorMentionsPathRestriction(validationIssueText(resp))
}

func operatorErrorMentionsPathRestriction(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "path") &&
		(strings.Contains(lower, "restrict") ||
			strings.Contains(lower, "outside") ||
			strings.Contains(lower, "system director"))
}
