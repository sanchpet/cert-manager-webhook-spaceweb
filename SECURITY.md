# Security Policy

## Reporting a vulnerability

Please report suspected vulnerabilities **privately**, not in a public issue.
Use GitHub's
[private vulnerability reporting](https://github.com/sanchpet/cert-manager-webhook-spaceweb/security/advisories/new)
for this repository. Include a description, affected version, and reproduction
steps where possible. You will get an acknowledgement, and a fix or mitigation
will be coordinated before any public disclosure.

## Credential-breadth caveat

This webhook authenticates to SpaceWeb with your **control-panel login and
password**. SpaceWeb offers no scoped API token or per-zone credential, so the
credential the solver holds administers your **entire SpaceWeb account** — not
just DNS, and not just one zone.

Consequences to plan for:

- The `sweb-credentials` Secret is a high-value target. Restrict RBAC so only
  cert-manager can read it (see [`docs/troubleshooting.md`](docs/troubleshooting.md)),
  keep it out of version control, and prefer a Secret manager (e.g. External
  Secrets Operator) over a plaintext manifest.
- The password sits at rest in the Secret. Enable
  [encryption at rest](https://kubernetes.io/docs/tasks/administer-cluster/encrypt-data/)
  for Secrets in the cluster.
- If the credential is exposed, rotate the SpaceWeb account password promptly —
  that revokes it everywhere, since there is nothing narrower to revoke.

The SDK exchanges the password for a short-lived session token internally, but
that does not narrow what the stored credential can do.
