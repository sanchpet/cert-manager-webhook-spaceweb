//go:build conformance

// The cert-manager acme conformance fixture drives a REAL SpaceWeb zone through
// an envtest control plane. Its imported test/apiserver package panics in init()
// when the envtest binaries are absent — before any t.Skip can run — so the env
// gate alone cannot keep `go test ./...` green. The file is therefore also placed
// behind the `conformance` build tag, excluding it from the default build; run it
// explicitly with:
//
//	TEST_ZONE_NAME=example.com. go test -tags conformance -run TestRunsSuite ./...
package main

import (
	"os"
	"testing"
	"time"

	acmetest "github.com/cert-manager/cert-manager/test/acme"
)

// TestRunsSuite runs the cert-manager DNS-01 conformance suite against the live
// SpaceWeb API. Beyond the build tag it also skips without TEST_ZONE_NAME (and
// the real credentials in testdata/cert-manager-webhook-spaceweb/secret.yaml).
//
// SpaceWeb DNS propagation is slow (~20 min); the propagation limit is set
// generously so the record has time to resolve. Running it also needs the
// envtest control-plane binaries (setup-envtest / KUBEBUILDER_ASSETS).
func TestRunsSuite(t *testing.T) {
	zone := os.Getenv("TEST_ZONE_NAME")
	if zone == "" {
		t.Skip("TEST_ZONE_NAME not set; skipping live conformance suite")
	}

	fixture := acmetest.NewFixture(&spacewebSolver{},
		acmetest.SetResolvedZone(zone),
		acmetest.SetManifestPath("testdata/cert-manager-webhook-spaceweb"),
		acmetest.SetPollInterval(30*time.Second),
		acmetest.SetPropagationLimit(30*time.Minute),
	)

	fixture.RunConformance(t)
}
