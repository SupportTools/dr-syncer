# Web UI

## Overview

DR-Syncer includes an optional web dashboard deployed on the control cluster for monitoring replication status, managing CRDs, and triggering DR operations — without needing kubectl access.

The Web UI is a **separate deployment** (own pod, own Docker image) consisting of a Go API server (Chi router) that serves both a REST API and an embedded React single-page application.

## Architecture

```
+---------------------------------------------------+
|                  Web UI Pod                        |
|  +---------------------------------------------+  |
|  |         Go API Server (Chi)                  |  |
|  |  :8443 --- REST API + WebSocket              |  |
|  |  :8080 --- Prometheus metrics                |  |
|  |  :8081 --- Health probes                     |  |
|  +---------------------------------------------+  |
|  +---------------------------------------------+  |
|  |    Embedded React SPA (static files)         |  |
|  +---------------------------------------------+  |
+---------------------------------------------------+
         |                    |
    K8s API            GitHub OAuth
    (CRDs, Secrets)    (external)
```

### Components

- **Go API Server**: Chi router serving REST endpoints for CRD CRUD, DR operations, user management, and WebSocket for real-time updates
- **React Frontend**: Vite + React + TypeScript + shadcn/ui embedded in the Go binary via `embed.FS`
- **Authentication**: Local auth (K8s Secret-backed) + GitHub OAuth2, JWT tokens (15min access + 7d refresh)
- **WebSocket**: Real-time CRD status updates pushed from K8s informers to connected clients

## Features

### Dashboard
- Cluster connectivity status (health indicators)
- Active sync operations with real-time progress bars
- Recent operation history
- Quick-action buttons for common DR operations

### CRD Management
- Full CRUD for NamespaceMappings, ClusterMappings, BackupRepositories
- Read-only views for RemoteClusters, PVCSyncOperations, VolumeBackupOperations
- Real-time status updates via WebSocket

### DR Operations
- Trigger Stage, Cutover, and Failback operations from the UI
- Wizard-style flow: Select mapping -> Choose operation -> Confirm -> Live progress
- Real-time log output and cancel support

### User Management
- Local users stored in K8s Secret with bcrypt password hashing
- GitHub OAuth2 single sign-on
- Three RBAC roles: Admin, Operator, Viewer

## Authentication

### Local Auth
Users are stored in a K8s Secret (`dr-syncer-ui-users`) in the `dr-syncer-system` namespace. The secret contains a `users.json` key with user records including username, bcrypt hash, and role.

The user list is cached in-memory on startup and hot-reloaded via a K8s informer watch on the Secret.

### GitHub OAuth
Standard OAuth2 Authorization Code flow. GitHub users must be pre-provisioned in the local user store (matched by GitHub user ID). No auto-provisioning in v1.

### JWT Tokens
- **Access token**: 15-minute expiry, contains user ID, username, and role
- **Refresh token**: 7-day expiry, stored server-side
- **Signing**: HS256 with a secret stored in K8s Secret `dr-syncer-ui-jwt-secret`

## RBAC

| Role | CRD Read | CRD Write | Trigger Ops | User Mgmt |
|------|----------|-----------|-------------|-----------|
| Viewer | Yes | No | No | No |
| Operator | Yes | Yes | Yes | No |
| Admin | Yes | Yes | Yes | Yes |

## Configuration

### Enabling the Web UI

```yaml
# values.yaml
ui:
  enabled: true
  replicaCount: 1
  image:
    repository: docker.io/supporttools/dr-syncer-ui
    tag: "latest"
  service:
    type: ClusterIP
    port: 8443
  ingress:
    enabled: false
    className: ""
    hosts:
      - host: dr-syncer.example.com
        paths:
          - path: /
            pathType: Prefix
    tls: []
  auth:
    jwtSecret: ""          # Auto-generated if empty
    github:
      enabled: false
      clientID: ""
      clientSecret: ""
      allowedOrgs: []      # Restrict to specific GitHub orgs
  resources:
    requests:
      cpu: 100m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 256Mi
```

### Accessing the UI

With the default `ClusterIP` service:

```bash
kubectl port-forward svc/dr-syncer-ui -n dr-syncer-system 8443:8443
# Open https://localhost:8443
```

With Ingress enabled, access via the configured hostname.

### Default Admin User

On first startup, if no user Secret exists, the UI server creates a default admin user:
- Username: `admin`
- Password: `admin` (change immediately)

## API Endpoints

### Public
| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/auth/login` | Local login (username/password -> JWT) |
| POST | `/api/v1/auth/refresh` | Refresh access token |
| GET | `/api/v1/auth/github` | Initiate GitHub OAuth flow |
| GET | `/api/v1/auth/github/callback` | GitHub OAuth callback |
| POST | `/api/v1/auth/logout` | Invalidate refresh token |

### Authenticated (JWT Required)
| Method | Path | Description | Min Role |
|--------|------|-------------|----------|
| GET | `/api/v1/me` | Current user info | Viewer |
| GET | `/api/v1/clusters` | List RemoteClusters | Viewer |
| GET/POST/PUT/DELETE | `/api/v1/namespace-mappings` | NamespaceMapping CRUD | Operator (write) |
| GET/POST/PUT/DELETE | `/api/v1/cluster-mappings` | ClusterMapping CRUD | Operator (write) |
| GET | `/api/v1/pvc-sync-ops` | List PVCSyncOperations | Viewer |
| GET/POST/PUT/DELETE | `/api/v1/backup-repos` | BackupRepository CRUD | Operator (write) |
| GET | `/api/v1/volume-backup-ops` | List VolumeBackupOperations | Viewer |
| POST | `/api/v1/operations/stage` | Trigger Stage | Operator |
| POST | `/api/v1/operations/cutover` | Trigger Cutover | Operator |
| POST | `/api/v1/operations/failback` | Trigger Failback | Operator |
| GET/POST/PUT/DELETE | `/api/v1/users` | User management | Admin |
| WS | `/api/v1/ws` | Real-time CRD updates | Viewer |

## Real-Time Updates

The WebSocket endpoint at `/api/v1/ws` provides real-time CRD change notifications. Messages follow this format:

```json
{
  "type": "crd_update",
  "resource": "namespacemapping",
  "action": "modified",
  "data": { ... }
}
```

The frontend uses these events to update the TanStack Query cache directly, providing instant UI updates without polling.

## Tech Stack

### Backend
- Go + Chi router (net/http compatible)
- K8s client-go (dynamic + typed clients)
- gorilla/websocket for WebSocket
- golang-jwt for JWT tokens
- golang.org/x/oauth2 for GitHub OAuth

### Frontend
- Vite + React 18 + TypeScript
- shadcn/ui (Tailwind CSS + Radix UI)
- TanStack Query v5 (server state)
- Zustand (client state)
- React Router v6
- React Hook Form + Zod (form validation)
- Recharts (metrics visualization)
