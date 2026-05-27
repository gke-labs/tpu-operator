package tpunodegroup

import (
	_ "embed"
	"strings"
)

// startupScriptFmt holds the raw bash script for K8s node initialization.
//
//go:embed startup_script.sh
var startupScriptFmt string

// renderStartupScript returns the templated K8s initialization script for a given version.
func renderStartupScript(version string) string {
	return strings.ReplaceAll(startupScriptFmt, "{{VERSION}}", version)
}
