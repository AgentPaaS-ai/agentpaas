package doctor

import (
	"fmt"
	"os/exec"
)

// CheckSkopeo checks whether the optional skopeo binary is available.
// skopeo is a lazy dependency: only required for `agentpaas install --prefer-image`
// (load prebuilt bundle image) and `agentpaas export --with-image` (embed image
// in bundle). The default install path rebuilds from source via docker build
// and does not need skopeo.
//
// This check is info-level (warning, not error) and never fails the gate.
func CheckSkopeo() CheckResult {
	path, err := exec.LookPath("skopeo")
	if err != nil {
		return CheckResult{
			Status:  "warning",
			Name:    "skopeo (optional)",
			Message: "not found (needed for prebuilt image install/export; brew install skopeo)",
			FixHint: "brew install skopeo",
		}
	}
	return CheckResult{
		Status:  "ok",
		Name:    "skopeo (optional)",
		Message: fmt.Sprintf("found at %s", path),
	}
}
