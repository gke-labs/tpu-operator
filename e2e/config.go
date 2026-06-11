package e2e

import (
	"flag"
	"os"
	"strings"
)


type TestConfig struct {
	Project        string
	Zone           string
	Region         string
	Reservation    string
	ControlPlaneIP string
}

var Config TestConfig

func init() {
	flag.StringVar(&Config.Project, "e2e-project", "", "GCP Project ID for E2E tests")
	flag.StringVar(&Config.Zone, "e2e-zone", "", "GCE Zone for E2E tests")
	flag.StringVar(&Config.Region, "e2e-region", "", "GCE Region for E2E tests")
	flag.StringVar(&Config.Reservation, "e2e-reservation", "", "GCP Reservation for E2E tests")
	flag.StringVar(&Config.ControlPlaneIP, "e2e-control-plane-ip", "", "Control plane IP for E2E tests")
}

// BindEnv falls back to env if flags are not set.
func (c *TestConfig) BindEnv() {
	if c.Project == "" {
		c.Project = os.Getenv("E2E_PROJECT")
	}
	if c.Zone == "" {
		c.Zone = os.Getenv("E2E_ZONE")
	}
	if c.Region == "" {
		if envRegion := os.Getenv("E2E_REGION"); envRegion != "" {
			c.Region = envRegion
		} else {
			c.Region = "us-central1"
		}
	}
	if c.Reservation == "" {
		c.Reservation = os.Getenv("E2E_RESERVATION")
	}
	if c.ControlPlaneIP == "" {
		c.ControlPlaneIP = os.Getenv("E2E_CONTROL_PLANE_IP")
	}
}

// expandManifest replaces the placeholders in the manifest YAML
// with the values from the test configuration.
func expandManifest(yamlBytes []byte) []byte {
	replacer := strings.NewReplacer(
		"${E2E_PROJECT}", Config.Project,
		"${E2E_ZONE}", Config.Zone,
		"${E2E_REGION}", Config.Region,
		"${E2E_RESERVATION}", Config.Reservation,
		"${E2E_CONTROL_PLANE_IP}", Config.ControlPlaneIP,
	)
	return []byte(replacer.Replace(string(yamlBytes)))
}
