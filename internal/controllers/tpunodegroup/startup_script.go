package tpunodegroup

import (
	_ "embed"
	"strings"
)

// startupScriptFmt holds the raw bash script for K8s node initialization.
//
//go:embed startup_script.sh
var startupScriptFmt string

// RenderStartupScript returns the templated K8s initialization script for a given version, project and zone.
func RenderStartupScript(version, project, zone string) string {
	script := strings.ReplaceAll(startupScriptFmt, "{{VERSION}}", version)
	script = strings.ReplaceAll(script, "{{PROJECT}}", project)
	script = strings.ReplaceAll(script, "{{ZONE}}", zone)
	return script
}
