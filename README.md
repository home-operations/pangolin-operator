# pangolin-operator

A Kubernetes operator that manages [Pangolin](https://github.com/fosrl/pangolin) tunnel infrastructure through native Kubernetes resources. It provisions Pangolin sites, manages the [newt](https://github.com/fosrl/newt) tunnel Deployment, and continuously reconciles public and private resources against the Pangolin API.

## How it works

```
┌────────────────────────────────────────────────────┐
│  Kubernetes cluster                                │
│                                                    │
│  NewtSite CR ──► creates Pangolin site             │
│               ──► deploys newt Deployment          │
│               ──► watches HTTPRoutes / Services    │
│                                                    │
│  PublicResource CR ──► Pangolin public resource    │
│                         (HTTP / TCP / UDP)         │
│                                                    │
│  PrivateResource CR ──► Pangolin site resource     │
│                          (OLM VPN: host/cidr/port) │
└────────────────────────────────────────────────────┘
```

The operator calls the Pangolin REST API directly. No blueprint files, no sidecars.

## CRDs

| Kind | Short name | Description |
|---|---|---|
| `NewtSite` | `nsite` | Pangolin site + newt tunnel Deployment |
| `PublicResource` | `pubr` | Pangolin public resource (HTTP, TCP, UDP) |
| `PrivateResource` | `privr` | Pangolin private/OLM resource |

All CRDs are namespaced and live under `pangolin.home-operations.com/v1alpha1`.

## Prerequisites

### Enable the Pangolin Integration API

This operator communicates exclusively with the [Pangolin Integration API](https://docs.pangolin.net/self-host/advanced/integration-api). The Integration API is **disabled by default** in Pangolin — you must enable it before deploying the operator.

In your Pangolin `config.yml`:

```yaml
flags:
  enable_integration_api: true
```

The API listens on port `3003` by default. Expose it via Traefik (or another reverse proxy) so the operator can reach it. The `PANGOLIN_API_URL` environment variable should point to the exposed base URL (e.g. `https://api.example.com`).

See the [Pangolin Integration API docs](https://docs.pangolin.net/self-host/advanced/integration-api) for the full setup including Traefik routing configuration and Swagger UI access.

## Configuration

The operator reads its Pangolin credentials from environment variables:

| Variable | Description |
|---|---|
| `PANGOLIN_API_URL` | Pangolin API base URL (e.g. `https://pangolin.example.com`) |
| `PANGOLIN_API_KEY` | Pangolin API key |
| `PANGOLIN_ORG_ID` | Pangolin organisation ID |
| `PANGOLIN_ENDPOINT` | Endpoint passed to newt pods (`PANGOLIN_ENDPOINT` env var) |

## NewtSite

A `NewtSite` provisions a Pangolin site and — unless `type: local` — manages a `Deployment` running the newt tunnel daemon.

```yaml
apiVersion: pangolin.home-operations.com/v1alpha1
kind: NewtSite
metadata:
  name: homelab
  namespace: network
spec:
  name: Homelab
  type: newt          # "newt" (default) or "local"
  newt:
    image: ghcr.io/fosrl/newt
    tag: latest
    replicas: 1
    logLevel: INFO    # DEBUG | INFO | WARN | ERROR
    resources:
      requests:
        cpu: 10m
        memory: 32Mi
  autoDiscover:
    annotationPrefix: pangolin-operator   # default
    enableRouteDiscovery: false           # enable HTTPRoute auto-discovery (default: false)
    enableServiceDiscovery: false         # enable Service auto-discovery (default: false)
    gatewayName: envoy-gateway            # filter HTTPRoutes by parentRef gateway
    gatewayNamespace: network
    gatewayTargetHostname: envoy-external.network.svc.cluster.local  # override target hostname for gateway-based discovery
    ssl: true              # default SSL for HTTP resources
    denyCountries: "RU,CN,KP,IR"
```

The operator auto-creates a `Secret` named `<site>-newt-credentials` containing `PANGOLIN_ENDPOINT`, `NEWT_ID`, and `NEWT_SECRET`. The newt `Deployment` reads credentials from this Secret.

### WireGuard native interface

Set `newt.useNativeInterface: true` to use the kernel WireGuard module instead of the userspace implementation. This runs the pod as root with `NET_ADMIN` and `SYS_MODULE` capabilities. Only use this when the node kernel has the WireGuard module loaded.

```yaml
spec:
  newt:
    useNativeInterface: true
    hostNetwork: true   # optional: grant host network namespace
    hostPID: false
```

## PublicResource

Manages a Pangolin public resource. The `siteRef` field references a `NewtSite` in the same (or another) namespace.

### HTTP

```yaml
apiVersion: pangolin.home-operations.com/v1alpha1
kind: PublicResource
metadata:
  name: my-app
  namespace: default
spec:
  siteRef: homelab
  name: My App
  protocol: http
  fullDomain: app.example.com
  ssl: true
  targets:
    - hostname: my-app.default.svc.cluster.local
      port: 8080
      method: http   # http | https | h2c
```

### TCP / UDP

```yaml
spec:
  siteRef: homelab
  name: Forgejo SSH
  protocol: tcp      # tcp | udp
  proxyPort: 2222
  targets:
    - hostname: forgejo.selfhosted.svc.cluster.local
      port: 22
```

### Auth

```yaml
spec:
  auth:
    ssoEnabled: true
    ssoRoles:
      - Member
    autoLoginIdp: 1
    authSecretRef: myapp-auth   # Kubernetes Secret name
```

**Secret keys** — `pincode`, `password`, `basic-auth-user`, `basic-auth-password`.

### Access control rules

```yaml
spec:
  rules:
    - action: DROP
      match: country
      value: RU
    - action: ACCEPT
      match: cidr
      value: 10.0.0.0/8
      priority: 10
```

Valid `action` values: `ACCEPT`, `DROP`, `PASS`. Valid `match` values: `ip`, `cidr`, `path`, `country`.

### Cross-namespace site reference

```yaml
spec:
  siteRef: homelab
  siteNamespace: network   # defaults to PublicResource's own namespace
```

## PrivateResource

Registers a host, CIDR range, or port with the Pangolin OLM VPN. Clients with the appropriate roles gain access through the newt tunnel.

```yaml
apiVersion: pangolin.home-operations.com/v1alpha1
kind: PrivateResource
metadata:
  name: cluster-pods
  namespace: network
spec:
  siteRef: homelab
  name: Cluster Pod Network
  mode: cidr              # host | cidr | port
  destination: 10.42.0.0/16
  tcpPorts: "*"
  udpPorts: "*"
  disableIcmp: false
  roleIds: [1, 2]
  userIds: []
  clientIds: []
```

In `host` mode, `destination` can be an IP address or a hostname. If it is a hostname, `alias` (a FQDN) is required.

## Auto-discovery

When `autoDiscover` is set on a `NewtSite`, the operator can watch `HTTPRoute` and `Service` resources and automatically create `PublicResource` CRs owned by the `NewtSite`. Both discovery modes are **disabled by default** and must be explicitly enabled.

| Field | Default | Description |
|---|---|---|
| `enableRouteDiscovery` | `false` | Enable HTTPRoute auto-discovery |
| `enableServiceDiscovery` | `false` | Enable Service auto-discovery |

### HTTPRoute

HTTPRoute discovery is enabled by setting `enableRouteDiscovery: true` on `autoDiscover`. The operator processes every `HTTPRoute` hostname as a separate `PublicResource`. The backend target is derived from the first `backendRef` in the first rule.

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: my-app
  namespace: default
  annotations:
    pangolin-operator/site-ref: homelab
spec:
  hostnames:
    - app.example.com
  rules:
    - backendRefs:
        - name: my-app
          port: 8080
```

#### HTTPRoute annotations

| Annotation | Description |
|---|---|
| `pangolin-operator/site-ref` | Name of the `NewtSite` to use (required unless matched by gateway) |
| `pangolin-operator/site-namespace` | Namespace of the `NewtSite` (defaults to route namespace) |
| `pangolin-operator/enabled: "false"` | Opt out — skip this route |
| `pangolin-operator/name` | Override resource display name (defaults to route name) |
| `pangolin-operator/ssl: "false"` | Disable SSL |
| `pangolin-operator/method` | Internal backend protocol: `http`, `https`, or `h2c` (default `http`) |
| `pangolin-operator/host-header` | Override the `Host` header sent to the backend |
| `pangolin-operator/tls-server-name` | Override the TLS SNI name (defaults to the hostname) |
| `pangolin-operator/headers` | JSON array of extra headers: `[{"name":"X-Foo","value":"bar"}]` |
| `pangolin-operator/auth-sso: "true"` | Enable SSO authentication |
| `pangolin-operator/auth-sso-roles` | Comma-separated Pangolin roles (overrides site default) |
| `pangolin-operator/auth-sso-users` | Comma-separated user e-mails (overrides site default) |
| `pangolin-operator/auth-sso-idp` | Pangolin IdP ID for auto-login (overrides site default) |
| `pangolin-operator/auth-whitelist-users` | Comma-separated user e-mails for whitelist |
| `pangolin-operator/auth-secret` | Kubernetes Secret name containing sensitive auth values |
| `pangolin-operator/maintenance-enabled: "true"` | Enable maintenance page |
| `pangolin-operator/maintenance-type` | `forced` or `automatic` |
| `pangolin-operator/maintenance-title` | Maintenance page title |
| `pangolin-operator/maintenance-message` | Maintenance page body |
| `pangolin-operator/maintenance-estimated-time` | Estimated duration |
| `pangolin-operator/rules` | JSON array of access control rules |
| `pangolin-operator/target-path` | Target path prefix, exact path, or regex |
| `pangolin-operator/target-path-match` | `prefix`, `exact`, or `regex` |
| `pangolin-operator/target-rewrite-path` | Rewrite request path to this value |
| `pangolin-operator/target-rewrite-match` | `exact`, `prefix`, `regex`, or `stripPrefix` |
| `pangolin-operator/target-priority` | Load-balancing priority (1–1000) |
| `pangolin-operator/target-enabled` | `"true"` or `"false"` to enable/disable the target |

### Service

Services can be exposed in TCP/UDP mode or HTTP mode (when `pangolin-operator/full-domain` is set).

Service discovery is enabled by setting `enableServiceDiscovery: true` on `autoDiscover`. Once enabled, any Service annotated with `pangolin-operator/site-ref` is discovered. Annotate with `pangolin-operator/enabled: "false"` to exclude a specific Service.

#### Service annotations

| Annotation | Description |
|---|---|
| `pangolin-operator/site-ref` | Name of the `NewtSite` (required) |
| `pangolin-operator/site-namespace` | Namespace of the `NewtSite` |
| `pangolin-operator/enabled` | `"true"` to opt in; `"false"` to opt out |
| `pangolin-operator/full-domain` | Public domain — activates HTTP mode |
| `pangolin-operator/port` | Port number or name to expose (required when Service has multiple ports and none named `http`) |
| `pangolin-operator/protocol` | `tcp` or `udp` (TCP/UDP mode only) |
| `pangolin-operator/all-ports: "true"` | Expose every Service port as a separate resource |
| `pangolin-operator/name` | Override resource display name |
| `pangolin-operator/method` | HTTP mode: `http`, `https`, or `h2c` |
| `pangolin-operator/ssl` | HTTP mode: enable/disable SSL |
| `pangolin-operator/host-header` | HTTP mode: override Host header |
| `pangolin-operator/tls-server-name` | HTTP mode: override TLS SNI |
| `pangolin-operator/headers` | HTTP mode: JSON array of extra headers |
| `pangolin-operator/auth-sso` | HTTP mode: enable SSO |
| `pangolin-operator/auth-sso-roles` | HTTP mode: SSO roles |
| `pangolin-operator/auth-sso-users` | HTTP mode: SSO users |
| `pangolin-operator/auth-sso-idp` | HTTP mode: auto-login IdP ID |
| `pangolin-operator/auth-whitelist-users` | HTTP mode: whitelist users |
| `pangolin-operator/auth-secret` | HTTP mode: Secret name for sensitive auth |
| `pangolin-operator/maintenance-enabled` | HTTP mode: enable maintenance page |
| `pangolin-operator/rules` | HTTP mode: JSON access control rules |

#### Port selection (single-port mode)

When `pangolin-operator/port` is not set, the operator selects a port automatically:

1. Service has exactly one port → use it
2. Service has a port named `http` → use it
3. Otherwise the Service is skipped

### Gateway-based discovery

Instead of annotating every `HTTPRoute` with `site-ref`, set `gatewayName` on the `NewtSite`. The operator will process every `HTTPRoute` whose `spec.parentRefs` references that gateway, using the `NewtSite` name as the implicit site reference.

```yaml
spec:
  autoDiscover:
    gatewayName: envoy-gateway
    gatewayNamespace: network
```

Individual routes can still override with `pangolin-operator/site-ref` or opt out with `pangolin-operator/enabled: "false"`.

### Custom annotation prefix

To avoid conflicts when running multiple operators or sites, set `annotationPrefix` on the `NewtSite`:

```yaml
spec:
  autoDiscover:
    annotationPrefix: myorg
```

Then annotate resources with `myorg/site-ref`, `myorg/enabled`, etc.

## Deployment

### Install via Helm

```bash
helm install pangolin-operator oci://ghcr.io/home-operations/charts/pangolin-operator \
  --namespace pangolin-operator --create-namespace \
  --set env.PANGOLIN_API_URL=https://pangolin.example.com \
  --set env.PANGOLIN_API_KEY=<key> \
  --set env.PANGOLIN_ORG_ID=<org-id> \
  --set env.PANGOLIN_ENDPOINT=https://pangolin.example.com
```

### Flux CD example

```yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: pangolin-operator
  namespace: flux-system
spec:
  interval: 15m
  ref:
    tag: 0.1.0
  url: oci://ghcr.io/home-operations/charts/pangolin-operator
---
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: pangolin-operator
  namespace: pangolin-operator
spec:
  chartRef:
    kind: OCIRepository
    name: pangolin-operator
    namespace: flux-system
  interval: 1h
  values:
    env:
      PANGOLIN_API_URL: https://pangolin.example.com
      PANGOLIN_ORG_ID: <org-id>
    envFrom:
      - secretRef:
          name: pangolin-operator-credentials   # keys: PANGOLIN_API_KEY, PANGOLIN_ENDPOINT
```

## Quick start

1. Deploy the operator (see above).
2. Create a `NewtSite` — the operator provisions the Pangolin site and deploys newt.
3. Annotate `HTTPRoute` or `Service` resources, or create `PublicResource` / `PrivateResource` CRs directly.

```yaml
# 1. Site
apiVersion: pangolin.home-operations.com/v1alpha1
kind: NewtSite
metadata:
  name: homelab
  namespace: network
spec:
  name: Homelab
  autoDiscover:
    enableRouteDiscovery: true
    gatewayName: envoy-gateway
    ssl: true
    denyCountries: "RU,CN,KP,IR"
---
# 2. Static public resource (no HTTPRoute needed)
apiVersion: pangolin.home-operations.com/v1alpha1
kind: PublicResource
metadata:
  name: forgejo-ssh
  namespace: network
spec:
  siteRef: homelab
  name: Forgejo SSH
  protocol: tcp
  proxyPort: 2222
  targets:
    - hostname: forgejo.selfhosted.svc.cluster.local
      port: 22
---
# 3. Private OLM resource
apiVersion: pangolin.home-operations.com/v1alpha1
kind: PrivateResource
metadata:
  name: cluster-pods
  namespace: network
spec:
  siteRef: homelab
  name: Cluster Pod Network
  mode: cidr
  destination: 10.42.0.0/16
```
