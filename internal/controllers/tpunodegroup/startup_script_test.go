package tpunodegroup

import (
	"strings"
	"testing"
)

func TestRenderStartupScript(t *testing.T) {
	version := "1.31"
	project := "my-project"
	zone := "us-central1-a"

	script := renderStartupScript(version, project, zone)

	if !strings.Contains(script, version) {
		t.Errorf("Expected script to contain version %s", version)
	}
	if !strings.Contains(script, project) {
		t.Errorf("Expected script to contain project %s", project)
	}
	if !strings.Contains(script, zone) {
		t.Errorf("Expected script to contain zone %s", zone)
	}

	if strings.Contains(script, "{{VERSION}}") {
		t.Errorf("Expected script to not contain placeholder {{VERSION}}")
	}
	if strings.Contains(script, "{{PROJECT}}") {
		t.Errorf("Expected script to not contain placeholder {{PROJECT}}")
	}
	if strings.Contains(script, "{{ZONE}}") {
		t.Errorf("Expected script to not contain placeholder {{ZONE}}")
	}
}
