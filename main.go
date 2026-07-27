// Command cert-manager-webhook-spaceweb is a cert-manager ACME DNS-01 solver
// webhook for the SpaceWeb (sweb.ru) DNS provider. It serves the apiserver that
// cert-manager calls to Present/CleanUp _acme-challenge TXT records.
//
// The binary mirrors github.com/cert-manager/webhook-example: main just wires
// the group name (from the GROUP_NAME env, matching the APIService group and the
// Issuer's dns01.webhook.groupName) into cmd.RunWebhookServer alongside the
// solver implementation.
package main

import (
	"fmt"
	"os"

	"github.com/cert-manager/cert-manager/pkg/acme/webhook/cmd"
)

// GroupName is the API group the webhook serves under. It must equal the
// APIService group, the GROUP_NAME env, and the Issuer's dns01.webhook.groupName
// (acme.sweb.ru). Sourced from the environment so the deployment controls it.
var GroupName = os.Getenv("GROUP_NAME")

// version is stamped at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if GroupName == "" {
		panic("GROUP_NAME must be specified")
	}

	fmt.Fprintf(os.Stderr, "cert-manager-webhook-spaceweb %s starting (group %q)\n", version, GroupName)

	// RunWebhookServer blocks, serving the ACME webhook apiserver until the
	// process is signalled to shut down.
	cmd.RunWebhookServer(GroupName, &spacewebSolver{})
}
