# Installation

This guide installs the SpaceWeb DNS-01 webhook solver and wires it to an ACME
issuer. It assumes cert-manager `v1.20.x` is already running.

## 1. Verify cert-manager

```sh
kubectl get pods -n cert-manager
kubectl get crd certificates.cert-manager.io
```

All cert-manager Pods should be `Running`, including `cert-manager-webhook` and
`cert-manager-cainjector` (this solver depends on the CA injector).

## 2. Install the chart

The Helm chart is an OCI artifact. Install it into the **same namespace as
cert-manager** (default `cert-manager`):

```sh
helm install cert-manager-webhook-spaceweb \
  oci://ghcr.io/sanchpet/charts/cert-manager-webhook-spaceweb \
  --namespace cert-manager \
  --set groupName=acme.sweb.ru
```

Pin a version with `--version <x.y.z>`. Useful values:

| Value | Default | Purpose |
|---|---|---|
| `groupName` | `acme.sweb.ru` | API group of the aggregated apiserver; **must** equal the Issuer `groupName` |
| `certManager.namespace` | `cert-manager` | Namespace cert-manager runs in |
| `certManager.serviceAccountName` | `cert-manager` | ServiceAccount cert-manager uses (the webhook grants it access) |
| `image.repository` | `ghcr.io/sanchpet/cert-manager-webhook-spaceweb` | Solver image |
| `image.tag` | chart appVersion | Image tag |

## 3. Confirm the APIService is healthy

The chart registers an `APIService` for `v1alpha1.acme.sweb.ru`. It must report
`Available=True` before any issuer will work:

```sh
kubectl get apiservice v1alpha1.acme.sweb.ru
```

```
NAME                     SERVICE                                          AVAILABLE   AGE
v1alpha1.acme.sweb.ru    cert-manager/cert-manager-webhook-spaceweb       True        1m
```

If it is not `Available`, see [troubleshooting.md](troubleshooting.md).

## 4. Create the credentials Secret

The solver reads SpaceWeb login + password from a Secret in the challenge's
namespace. For a **ClusterIssuer** that is cert-manager's namespace
(`cert-manager`); for a namespaced **Issuer** it is the Issuer's namespace.

```sh
kubectl create secret generic sweb-credentials \
  --namespace cert-manager \
  --from-literal=login='your-sweb-login' \
  --from-literal=password='your-sweb-password'
```

Or apply [`../examples/secret.yaml`](../examples/secret.yaml) after filling in
real values (do not commit them).

## 5. Create an issuer

Apply a ClusterIssuer with the `dns01.webhook` stanza — see
[`../examples/clusterissuer.yaml`](../examples/clusterissuer.yaml). Then check it
is ready:

```sh
kubectl apply -f examples/clusterissuer.yaml
kubectl get clusterissuer letsencrypt-sweb -o wide
```

Wait for `READY=True` (the ACME account registers on first use).

## 6. Request a certificate

Apply a Certificate (wildcard example in
[`../examples/certificate.yaml`](../examples/certificate.yaml)) and watch it:

```sh
kubectl apply -f examples/certificate.yaml
kubectl describe certificate example-com-wildcard
kubectl get challenges -A
```

Because SpaceWeb DNS propagation takes ~20 minutes, expect the Challenge to sit
in `pending` for a while. That is normal — see the propagation note in
[troubleshooting.md](troubleshooting.md).

## Uninstall

```sh
helm uninstall cert-manager-webhook-spaceweb -n cert-manager
```

Remove the issuer, Secret, and any Certificates separately if you no longer need
them.
