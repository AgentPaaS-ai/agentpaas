package pack

import _ "embed"

// Pack-owned scanner configs. Written to a temp dir outside the scanned tree
// on each invocation so the verdict depends only on these bytes and the tree.

//go:embed gitleaks_default.toml
var gitleaksDefaultConfig []byte

//go:embed gitleaks_deps.toml
var gitleaksDepsConfig []byte
