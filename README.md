# cert-manager-webhook-spaceweb

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

A [cert-manager](https://cert-manager.io) ACME **DNS-01** webhook solver for
[SpaceWeb](https://sweb.ru) (sweb.ru) — hosting DNS zones.

cert-manager cannot solve DNS-01 challenges for SpaceWeb out of the box: there
is no built-in solver for the SpaceWeb DNS API. This webhook plugs that gap. It
runs as an extension apiserver that cert-manager calls to publish and retract
the `_acme-challenge` TXT records Let's Encrypt (or any ACME CA) requires — so
you can issue certificates, **including wildcards** (`*.example.com`), for zones
hosted on SpaceWeb.

## Compatibility

| | |
|---|---|
| cert-manager | `v1.20.x` (built and tested against `v1.20.2`) |
| Kubernetes | any release cert-manager `v1.20` supports |
| Issuer `groupName` | `acme.sweb.ru` |
| Issuer `solverName` | `spaceweb` |
| Image | `ghcr.io/sanchpet/cert-manager-webhook-spaceweb` |

The webhook is an aggregated apiserver; its API group **must** match the
`groupName` you set on the Issuer. Both are `acme.sweb.ru` — do not change one
without the other.

## Prerequisites

- A running cert-manager (`v1.20.x`) in the cluster (default namespace
  `cert-manager`).
- A domain whose DNS zone is hosted at SpaceWeb.
- SpaceWeb control-panel credentials (login + password) that can edit that
  zone's records.

## Install

The chart is published as an OCI artifact. Install it into the **same namespace
as cert-manager** (default `cert-manager`) so RBAC and service discovery line
up:

```sh
helm install cert-manager-webhook-spaceweb \
  oci://ghcr.io/sanchpet/charts/cert-manager-webhook-spaceweb \
  --namespace cert-manager \
  --set groupName=acme.sweb.ru
```

`groupName` defaults to `acme.sweb.ru` in the chart; the `--set` above is
explicit for clarity. Pin a version with `--version`.

## Usage

Three objects: a Secret with your SpaceWeb credentials, an Issuer (or
ClusterIssuer) that points cert-manager at this solver, and a Certificate.

### 1. Credentials Secret

The solver reads a Secret from the challenge's namespace holding two keys,
`login` and `password`:

```sh
kubectl create secret generic sweb-credentials \
  --namespace cert-manager \
  --from-literal=login='your-sweb-login' \
  --from-literal=password='your-sweb-password'
```

For a **ClusterIssuer**, the Secret must live in the namespace cert-manager
runs in (its "cluster resource namespace", `cert-manager` by default). For a
namespaced **Issuer**, put the Secret in the same namespace as the Issuer.

A template is in [`examples/secret.yaml`](examples/secret.yaml) — placeholders
only, never commit real credentials.

### 2. ClusterIssuer

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-sweb
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: you@example.com
    privateKeySecretRef:
      name: letsencrypt-sweb-account-key
    solvers:
      - dns01:
          webhook:
            groupName: acme.sweb.ru
            solverName: spaceweb
            config:
              # apiUrl: https://api.sweb.ru   # optional, this is the default
              secretRef:
                name: sweb-credentials
```

See [`examples/clusterissuer.yaml`](examples/clusterissuer.yaml).

### 3. Wildcard Certificate

```yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: example-com-wildcard
  namespace: default
spec:
  secretName: example-com-wildcard-tls
  issuerRef:
    name: letsencrypt-sweb
    kind: ClusterIssuer
  dnsNames:
    - example.com
    - "*.example.com"
```

See [`examples/certificate.yaml`](examples/certificate.yaml). A wildcard SAN
alongside the apex makes cert-manager present two `_acme-challenge.example.com`
TXT records concurrently; the solver serialises writes per zone, so both land.

## Per-Issuer configuration

The `config` block under `dns01.webhook` is decoded by the solver:

| Field | Type | Required | Default | Meaning |
|---|---|---|---|---|
| `apiUrl` | string | no | `https://api.sweb.ru` | SpaceWeb API root |
| `secretRef.name` | string | yes | — | Secret (in the challenge's namespace) with keys `login` and `password` |

## How it works

cert-manager's DNS-01 flow delegates any solver it does not implement natively
to a **webhook**: a
[Kubernetes extension apiserver](https://kubernetes.io/docs/tasks/extend-kubernetes/setup-extension-api-server/)
registered as an `APIService` in the aggregation layer under the group
`acme.sweb.ru`. When a challenge is scheduled, cert-manager issues `Present` /
`CleanUp` calls against that aggregated API; the kube-apiserver proxies them to
this webhook Pod.

On **Present** the solver, using the SpaceWeb API:

1. splits `ResolvedFQDN` into the zone `domain` and the record `subDomain`;
2. lists the zone's records; if a TXT with the same name **and** value already
   exists it is a no-op (SpaceWeb's "add" is not idempotent), otherwise it adds
   the TXT with value `Key`.

On **CleanUp** it lists the zone, finds the TXT whose value equals the
challenge `Key`, and removes exactly that record by its positional index —
never a guessed index, since SpaceWeb indices are per-category positions, not
stable ids, and the add call does not return one.

The `caBundle` for the `APIService` is injected by cert-manager's CA injector
from the webhook's serving certificate, so the aggregation layer trusts the
Pod's TLS.

## SpaceWeb specifics

- **Auth is your control-panel login + password.** SpaceWeb has no scoped API
  token or per-zone credential: the same login+password that administers your
  whole account is what the solver holds. Treat the `sweb-credentials` Secret as
  a **broad, high-value credential** — restrict RBAC to it, avoid sharing the
  account, and rotate the password if it is exposed. (The SDK exchanges it for a
  short-lived session token internally and refreshes transparently, but the
  Secret still holds the full password.)
- **DNS propagation is slow — roughly 20 minutes.** The webhook itself is
  synchronous and fast: it just creates or deletes the TXT record and returns.
  Waiting for the record to become visible to the ACME CA is cert-manager's job,
  so give it room. Raise cert-manager's DNS-01 self-check / propagation timeout,
  e.g. start the cert-manager controller with a generous
  `--dns01-recursive-nameservers-only` self-check budget, and do not be alarmed
  by a challenge sitting in `pending` for many minutes — that is the zone
  propagating, not a failure. See
  [`docs/troubleshooting.md`](docs/troubleshooting.md).

## Development & testing

The local gate mirrors CI:

```sh
mise run ci      # lint + unit tests
```

Unit tests drive the solver against a mock SpaceWeb JSON-RPC server and need no
credentials. The **conformance** suite (cert-manager's `acmetest` fixture) talks
to the real SpaceWeb API and is excluded from the default build — it lives behind
the `conformance` build tag and additionally skips unless `TEST_ZONE_NAME` is
set:

```sh
# 1. Put real SpaceWeb control-panel credentials in the fixture Secret
#    (keys: login, password) — it is kubectl-applied into the test namespace:
$EDITOR testdata/cert-manager-webhook-spaceweb/secret.yaml

# 2. Provide envtest binaries (etcd/kube-apiserver) the fixture apiserver needs:
export KUBEBUILDER_ASSETS="$(setup-envtest use -p path)"

# 3. Run the tagged suite against a zone you control:
TEST_ZONE_NAME='example.com.' \
  go test -tags conformance -run TestRunsSuite ./...
```

The conformance run publishes and retracts a real TXT record in that zone.

## Credits

The SpaceWeb DNS API has no public specification. The record-editing semantics
this solver relies on were reverse-engineered by
[andreymal](https://github.com/andreymal) and
[curserio](https://github.com/curserio) for their `certbot-dns-sweb` plugins —
without that groundwork this webhook would not exist. Thank you.

DNS calls go through [`sweb-go-sdk`](https://github.com/sanchpet/sweb-go-sdk),
and the webhook scaffold follows cert-manager's
[webhook-example](https://github.com/cert-manager/webhook-example).

## License

MIT — see [LICENSE](LICENSE).
