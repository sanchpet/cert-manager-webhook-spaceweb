# Troubleshooting

Work top-down: the aggregation layer first (is the webhook reachable?), then
RBAC (can it read the Secret?), then the issuer config (does it name the solver
correctly?), then propagation (is it just slow?).

## APIService not `Available`

```sh
kubectl get apiservice v1alpha1.acme.sweb.ru
kubectl describe apiservice v1alpha1.acme.sweb.ru
```

If `AVAILABLE` is `False`:

- **Pod not running.** Check the webhook Pod and its logs:
  ```sh
  kubectl get pods -n cert-manager -l app.kubernetes.io/name=cert-manager-webhook-spaceweb
  kubectl logs -n cert-manager -l app.kubernetes.io/name=cert-manager-webhook-spaceweb
  ```
- **Service name / namespace mismatch.** The `APIService` points at a Service in
  a specific namespace. The chart must be installed in the **same namespace as
  cert-manager**; installing elsewhere breaks discovery.
- **Aggregation layer disabled.** Managed clusters occasionally disable the
  kube-apiserver aggregation layer. The `APIService` cannot become available
  without it.

## `caBundle` not injected

The `APIService` needs a `caBundle` so the kube-apiserver trusts the webhook's
TLS. cert-manager's **CA injector** fills it in from the webhook's serving
certificate. Symptoms: TLS / `x509` errors when cert-manager calls the solver,
or an empty `caBundle`.

```sh
kubectl get apiservice v1alpha1.acme.sweb.ru -o jsonpath='{.spec.caBundle}' | head -c 40
kubectl get pods -n cert-manager -l app.kubernetes.io/name=cainjector
```

- Ensure `cert-manager-cainjector` is running.
- Ensure the self-signed serving Certificate the chart creates is `Ready`:
  ```sh
  kubectl get certificate -n cert-manager
  ```
- A common cause is installing this chart into a namespace the cainjector's RBAC
  does not cover — keep it in `cert-manager`.

## RBAC: solver cannot read the Secret

The webhook runs Present/CleanUp under its own ServiceAccount and reads the
`sweb-credentials` Secret via the Kubernetes API. If RBAC does not grant it
`get` on that Secret, challenges fail. Logs show a `secrets "sweb-credentials"
is forbidden` error.

```sh
kubectl logs -n cert-manager -l app.kubernetes.io/name=cert-manager-webhook-spaceweb
```

- The chart ships a Role/RoleBinding granting the webhook `get` on Secrets in
  its namespace. If you moved the Secret to another namespace (e.g. for a
  namespaced Issuer), add a Role/RoleBinding there too.
- The Secret must be in the **challenge resource namespace**: cert-manager's
  namespace for a ClusterIssuer, the Issuer's namespace for an Issuer.
- Verify the ServiceAccount can read it:
  ```sh
  kubectl auth can-i get secret/sweb-credentials \
    --namespace cert-manager \
    --as system:serviceaccount:cert-manager:cert-manager-webhook-spaceweb
  ```

## Wrong `groupName` / `solverName`

cert-manager routes a webhook challenge by matching the Issuer's
`dns01.webhook.groupName` to a registered `APIService` group, and
`solverName` to the solver's advertised name. A mismatch means the challenge is
never dispatched (it stalls with "no solver" / "waiting for webhook").

- `groupName` **must** be `acme.sweb.ru` — the same string the chart registered
  (`--set groupName=...`). If you changed it on the chart, change it on every
  Issuer too.
- `solverName` **must** be `spaceweb`.

```sh
kubectl get clusterissuer letsencrypt-sweb -o yaml | grep -A6 webhook
```

## Secret key names

The solver reads exactly two keys, `login` and `password`. A `challenge` event
complaining about a missing key usually means the Secret was created with
different key names (e.g. `username`).

```sh
kubectl get secret sweb-credentials -n cert-manager -o jsonpath='{.data}' | tr ',' '\n'
```

You should see both `login` and `password`.

## Challenge stuck `pending` — propagation timeout

**SpaceWeb DNS propagation takes ~20 minutes.** The webhook publishes the TXT
record immediately; the delay is the authoritative zone updating, which is
outside this solver's control. cert-manager will not proceed until its own
self-check sees the record.

```sh
kubectl describe challenge -A
```

If you see `Waiting for DNS-01 challenge propagation` or a self-check timeout:

- **Be patient first.** Twenty minutes of `pending` is expected, not a bug.
- **Confirm the record exists at SpaceWeb** — check the DNS zone in the SpaceWeb
  control panel for a `_acme-challenge` TXT with the challenge value. If it is
  there, the solver did its job and you are only waiting on propagation.
- **Give cert-manager a generous self-check budget.** Propagation checks and
  timeouts are cert-manager's, not the webhook's. Point its self-check at an
  authoritative resolver and allow long enough for a slow zone, e.g.:
  ```
  --dns01-recursive-nameservers=1.1.1.1:53
  --dns01-recursive-nameservers-only
  ```
  and ensure the controller's challenge propagation timeout is not shorter than
  SpaceWeb's real propagation window.
- **Manually verify propagation** from outside the cluster:
  ```sh
  dig +short TXT _acme-challenge.example.com @1.1.1.1
  ```
  Empty for a while, then the value appears — that is propagation completing.

## CleanUp left a stale TXT record

CleanUp removes the TXT whose value matches the challenge key, by that record's
positional index. If a record was edited out-of-band between Present and CleanUp
the index shifts; the solver re-lists before removing to avoid deleting the
wrong record, so a genuinely missing record is a no-op. If you find an orphaned
`_acme-challenge` TXT, delete it in the SpaceWeb control panel — it is inert.
