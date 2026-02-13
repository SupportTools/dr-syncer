# Web UI - TaskForge Project Structure

**Project ID:** 90
**Topic:** Web UI - A web dashboard for the control cluster to monitor and manage DR operations
**Design Doc:** [docs/docs/web-ui.md](docs/web-ui.md)
**Plan:** /home/mmattox/.claude/plans/calm-rolling-balloon.md
**Created:** 2026-02-13
**Status:** Planning Complete

---

## Summary

| Metric | Value |
|--------|-------|
| Features | 7 |
| Tasks | 49 |
| Sprints | 4 (7 weeks total) |
| Total Estimate | ~43.5 days |
| Critical Path Length | 12 tasks |

---

## Feature Overview

| ID | Feature | Tasks | Sprint | Depends On | Estimate |
|----|---------|-------|--------|------------|----------|
| F1 | Project Setup & Build Infrastructure | T1-T6 (6) | Sprint 1 | -- | 3 days |
| F2 | Authentication & Authorization | T7-T12 (6) | Sprint 1 | F1 | 6.5 days |
| F3 | K8s Client & WebSocket | T13-T17 (5) | Sprint 1 | F1 | 5 days |
| F4 | REST API Endpoints | T18-T26 (9) | Sprint 2 | F2, F3 | 8.5 days |
| F5 | React Frontend - Core | T27-T33 (7) | Sprint 3 | F1 | 5 days |
| F6 | React Frontend - Pages | T34-T44 (11) | Sprint 3 | F4, F5 | 12.5 days |
| F7 | Helm Chart & Deployment | T45-T49 (5) | Sprint 4 | All | 3.5 days |

---

## Dependency Graph (Feature Level)

```
F1-ProjectSetup
  |
  +---> F2-Authentication ----+
  |                           |
  +---> F3-K8sClientWebSocket +---> F4-RESTAPI ------+
  |                                                    |
  +---> F5-FrontendCore ----+                          |
                            +---> F6-FrontendPages ----+---> F7-HelmDeployment
```

---

## Sprint Plan

### Sprint 1: Foundation (Weeks 1-2)

**Goal:** Establish the Go server with auth, K8s connectivity, and real-time WebSocket.

**Features:** F1-ProjectSetup, F2-Authentication, F3-K8sClientWebSocket
**Tasks:** T1-T17 (17 tasks, ~14.5 days)

**Critical Path:** T1 -> T2 -> T7 -> T9 -> T12

**Parallelization Opportunities:**
- T1 (Entrypoint) + T3 (Config) + T6 (Go Deps) can start in parallel
- After T2 (Server): T7 (UserStore), T13 (K8sClient), T15 (WSHub) can all proceed in parallel
- T8 (JWT) and T11 (Middleware) can proceed after T3 (Config)

### Sprint 2: API Layer (Weeks 3-4)

**Goal:** Complete REST API with full CRD CRUD, operation triggers, and test coverage.

**Features:** F4-RESTAPI
**Tasks:** T18-T26 (9 tasks, ~8.5 days)

**Critical Path:** T18-T25 (parallel) -> T26 (tests)

**Parallelization Opportunities:**
- T18-T22 (CRD endpoints) can all proceed in parallel
- T23 (Users), T24 (Operations), T25 (OpRunner) can proceed in parallel
- T26 (Tests) is the only serial bottleneck (requires all endpoints)

### Sprint 3: Frontend (Weeks 5-6)

**Goal:** Complete React frontend with all pages and real-time updates.

**Features:** F5-FrontendCore, F6-FrontendPages
**Tasks:** T27-T44 (18 tasks, ~17 days)

**Critical Path:** T27 -> T29 -> T30 -> T33 -> T38

**Parallelization Opportunities:**
- T28 (shadcn) and T29 (API Client) can start in parallel after T27
- T36 (TanStack Hooks) can start right after T29
- After T33 (Layout): all page tasks (T34-T44) can proceed in parallel
- Note: Frontend Core (T27-T33) can start during Sprint 2

### Sprint 4: Integration (Week 7)

**Goal:** Package into Helm chart, set up CI, integration testing.

**Features:** F7-HelmDeployment
**Tasks:** T45-T49 (5 tasks, ~3.5 days)

**Critical Path:** T45 -> T46 -> T48

**Note:** T49 (CI Workflow) only depends on T4 (Dockerfile) and can start earlier.

---

## Task Details

### F1: Project Setup & Build Infrastructure

#### T1: Create cmd/ui/main.go entrypoint
- **File:** `cmd/ui/main.go`
- **Priority:** Critical | **Estimate:** 0.5 day
- **Depends on:** None (start here)
- **Description:** Create the main.go entrypoint for the UI server. Parse flags/env vars, initialize config, create the Chi server, wire up signal handling (SIGTERM/SIGINT), start HTTP listener on configurable port (default :8443). Also start metrics (:8080) and health probe (:8081) servers.
- **Acceptance Criteria:**
  1. Binary compiles and starts successfully
  2. Graceful shutdown on SIGTERM
  3. Configurable via env vars (UI_PORT, UI_METRICS_PORT, UI_HEALTH_PORT)
  4. Logs startup info with structured logging

#### T2: Create pkg/ui/server.go with Chi router setup
- **File:** `pkg/ui/server.go`
- **Priority:** Critical | **Estimate:** 1 day
- **Depends on:** T1
- **Description:** Create the Chi router with full middleware stack: Logger, Recoverer, RequestID, RealIP, CORS (configurable origins), rate limiter (token bucket, 100 req/s per IP). Set up route groups for /api/v1/auth/* (public), /api/v1/* (JWT-protected), /healthz, /readyz, and catch-all for SPA static files via embed.FS.
- **Acceptance Criteria:**
  1. Chi router initializes with all middleware
  2. Health endpoints return 200
  3. SPA catch-all serves index.html for non-API routes
  4. CORS headers present in responses
  5. Rate limiter functional

#### T3: Create pkg/ui/config.go for UI server configuration
- **File:** `pkg/ui/config.go`
- **Priority:** High | **Estimate:** 0.5 day
- **Depends on:** None
- **Description:** Create configuration struct and loader. Support env vars: UI_PORT, UI_METRICS_PORT, UI_JWT_SECRET, UI_CORS_ORIGINS, UI_GITHUB_CLIENT_ID, UI_GITHUB_CLIENT_SECRET, UI_GITHUB_ALLOWED_ORGS, UI_RATE_LIMIT. Sensible defaults and validation.
- **Acceptance Criteria:**
  1. Config loads from env vars with defaults
  2. Validation errors are clear and actionable
  3. Sensitive values (secrets) not logged

#### T4: Create build/Dockerfile.ui multi-stage build
- **File:** `build/Dockerfile.ui`
- **Priority:** High | **Estimate:** 0.5 day
- **Depends on:** T1
- **Description:** Multi-stage Dockerfile: Stage 1 (Node 20 alpine) builds React frontend. Stage 2 (Go 1.23.5 alpine) builds Go binary with embedded frontend. Stage 3 (distroless:nonroot) final image. Support VERSION, GIT_COMMIT, BUILD_DATE build args.
- **Acceptance Criteria:**
  1. Docker build succeeds
  2. Final image is distroless/nonroot
  3. Binary starts in container
  4. Image size under 50MB (without frontend assets)

#### T5: Add Makefile targets for UI build
- **File:** `Makefile`
- **Priority:** High | **Estimate:** 0.5 day
- **Depends on:** T4
- **Description:** Add targets: build-ui, docker-build-ui, dev-ui. Update docker-build-all to include UI.
- **Acceptance Criteria:**
  1. `make build-ui` produces `bin/dr-syncer-ui`
  2. `make docker-build-ui` builds and tags image
  3. `make dev-ui` starts both Vite dev server and Go server
  4. `make docker-build-all` includes UI image

#### T6: Add Go dependencies for UI server
- **Files:** `go.mod`, `go.sum`
- **Priority:** High | **Estimate:** 0.5 day
- **Depends on:** None
- **Description:** Add: chi/v5, chi/cors, golang-jwt/v5, golang.org/x/oauth2, gorilla/websocket (promote from transitive).
- **Acceptance Criteria:**
  1. `go mod tidy` succeeds
  2. All imports resolve
  3. No unnecessary dependencies added

---

### F2: Authentication & Authorization

#### T7: Create pkg/ui/auth/user_store.go - K8s Secret-backed user store
- **File:** `pkg/ui/auth/user_store.go`
- **Priority:** Critical | **Estimate:** 1.5 days
- **Depends on:** T2, T3
- **Description:** UserStore reads from K8s Secret `dr-syncer-ui-users`. Data format: `users.json` key with `[{username, bcryptHash, role, githubID?}]`. In-memory cache with K8s informer hot-reload. Default admin user on first startup.
- **Acceptance Criteria:**
  1. Users load from K8s Secret on startup
  2. Default admin created if no Secret exists
  3. Hot-reload works when Secret is updated externally
  4. bcrypt password validation works
  5. CRUD operations persist to K8s Secret
  6. Thread-safe access to in-memory cache

#### T8: Create pkg/ui/auth/jwt.go - JWT token creation and validation
- **File:** `pkg/ui/auth/jwt.go`
- **Priority:** Critical | **Estimate:** 1 day
- **Depends on:** T3
- **Description:** HS256 JWT tokens. Access: 15min, contains {userID, username, role}. Refresh: 7d, server-side storage. JWT secret from K8s Secret (auto-generate if empty).
- **Acceptance Criteria:**
  1. Access tokens with correct claims and 15min expiry
  2. Refresh tokens stored server-side with 7d TTL
  3. Validation rejects expired/invalid tokens
  4. Revocation removes refresh token
  5. Background cleanup goroutine for expired tokens
  6. JWT secret loaded from K8s Secret

#### T9: Create pkg/ui/auth/handler.go - Auth HTTP handlers
- **File:** `pkg/ui/auth/handler.go`
- **Priority:** Critical | **Estimate:** 1 day
- **Depends on:** T7, T8
- **Description:** Handlers for: POST /login (validate creds, return JWT pair), POST /refresh, POST /logout. Rate limit: 10 req/min per IP on auth endpoints.
- **Acceptance Criteria:**
  1. Login returns tokens on valid credentials
  2. Login returns 401 on invalid credentials
  3. Refresh returns new access token
  4. Logout revokes refresh token
  5. Rate limiting prevents brute force
  6. Proper JSON error responses

#### T10: Create pkg/ui/auth/github_oauth.go - GitHub OAuth2 flow
- **File:** `pkg/ui/auth/github_oauth.go`
- **Priority:** High | **Estimate:** 1 day
- **Depends on:** T7, T8
- **Description:** OAuth2 Authorization Code flow: GET /github (redirect to GitHub), GET /github/callback (exchange code, fetch profile+orgs, match to local user, issue JWT). Reject unprovisioned GitHub users.
- **Acceptance Criteria:**
  1. Redirect to GitHub with correct scopes and state
  2. Callback exchanges code, fetches profile
  3. Matches GitHub user to local user by githubID
  4. Rejects unprovisioned GitHub users with 403
  5. Org restriction works when configured
  6. State param prevents CSRF

#### T11: Create pkg/ui/auth/middleware.go - JWT auth and RBAC middleware
- **File:** `pkg/ui/auth/middleware.go`
- **Priority:** Critical | **Estimate:** 1 day
- **Depends on:** T8
- **Description:** JWTAuth middleware (extract Bearer token, validate, inject user into context). RequireRole(role) factory (Viewer < Operator < Admin). Helpers: GetUserFromContext, GetRoleFromContext.
- **Acceptance Criteria:**
  1. Valid JWT passes through with user in context
  2. Missing/invalid/expired JWT returns 401
  3. RequireRole correctly enforces role hierarchy
  4. Context helpers return correct values
  5. Proper JSON error responses

#### T12: Unit tests for authentication components
- **Files:** `pkg/ui/auth/*_test.go`
- **Priority:** High | **Estimate:** 1 day
- **Depends on:** T7, T8, T9, T10, T11
- **Description:** Tests for JWT, UserStore, AuthMiddleware, AuthHandler, GitHub OAuth. Table-driven, mocked K8s client.
- **Acceptance Criteria:**
  1. >80% code coverage on auth package
  2. Happy paths and error cases covered
  3. Tests are deterministic
  4. K8s interactions properly mocked

---

### F3: K8s Client & WebSocket

#### T13: Create pkg/ui/k8s/client.go - K8s client manager
- **File:** `pkg/ui/k8s/client.go`
- **Priority:** High | **Estimate:** 1 day
- **Depends on:** T2, T3
- **Description:** Dynamic client for CRD CRUD (all DR-Syncer CRDs), typed client for Secrets/ConfigMaps. In-cluster service account config. Proper GVR mappings for api/v1alpha1.
- **Acceptance Criteria:**
  1. Client initializes from in-cluster config
  2. Dynamic client maps all DR-Syncer CRDs
  3. CRUD operations work for all CRD types
  4. Proper error handling (not-found, conflict, forbidden)
  5. Unstructured objects correctly converted

#### T14: Create pkg/ui/k8s/informers.go - CRD informers
- **File:** `pkg/ui/k8s/informers.go`
- **Priority:** High | **Estimate:** 1 day
- **Depends on:** T13
- **Description:** Informers for all CRDs. Event handlers (Add/Update/Delete) emit to channel consumed by WebSocket hub. Shared informer factory with resync period.
- **Acceptance Criteria:**
  1. Informers start and sync initial cache
  2. Events emitted to channel with type, action, data
  3. Graceful shutdown stops all informers
  4. Handles API server disconnection gracefully

#### T15: Create pkg/ui/ws/hub.go - WebSocket connection hub
- **File:** `pkg/ui/ws/hub.go`
- **Priority:** High | **Estimate:** 1 day
- **Depends on:** T2
- **Description:** Hub maintains active connections. Register/unregister clients. Broadcast. Ping/pong every 30s. JWT auth via query param. Max 100 connections (configurable).
- **Acceptance Criteria:**
  1. WebSocket upgrade works
  2. JWT validated on connection
  3. Broadcast sends to all clients
  4. Ping/pong keepalive at 30s
  5. Disconnected clients cleaned up
  6. Max connections enforced
  7. Thread-safe

#### T16: Create pkg/ui/ws/watcher.go - K8s informer to WebSocket bridge
- **File:** `pkg/ui/ws/watcher.go`
- **Priority:** High | **Estimate:** 1 day
- **Depends on:** T14, T15
- **Description:** Bridge informer events to WebSocket broadcasts. Format: `{type: "crd_update", resource, action, data}`. Filter sensitive fields. Debounce rapid updates (100ms).
- **Acceptance Criteria:**
  1. Informer events forwarded to WebSocket clients
  2. Message format matches spec
  3. Sensitive fields stripped
  4. Rapid updates debounced
  5. Handles hub unavailability gracefully

#### T17: Unit tests for WebSocket hub and watcher
- **Files:** `pkg/ui/ws/*_test.go`
- **Priority:** Medium | **Estimate:** 1 day
- **Depends on:** T15, T16
- **Description:** Tests for hub (connect, disconnect, broadcast, ping/pong, max connections) and watcher (event formatting, debouncing). Concurrent operation testing.
- **Acceptance Criteria:**
  1. >80% coverage on ws package
  2. Concurrent operations tested
  3. No goroutine leaks in tests

---

### F4: REST API Endpoints

#### T18: Create pkg/ui/api/clusters.go - RemoteCluster endpoints
- **File:** `pkg/ui/api/clusters.go`
- **Priority:** High | **Estimate:** 0.5 day
- **Depends on:** T11, T13
- **Description:** GET /clusters (list with status), GET /clusters/:id (detail). Viewer role. Pagination (limit/offset).
- **Acceptance Criteria:**
  1. List returns all RemoteClusters with status
  2. Detail returns full cluster info
  3. 404 for non-existent cluster
  4. Pagination works
  5. Viewer role sufficient

#### T19: Create pkg/ui/api/namespace_mappings.go - NamespaceMapping CRUD
- **File:** `pkg/ui/api/namespace_mappings.go`
- **Priority:** High | **Estimate:** 1 day
- **Depends on:** T11, T13
- **Description:** Full CRUD. Read=Viewer, Write=Operator. Validation. Sync status in responses.
- **Acceptance Criteria:**
  1. CRUD works correctly
  2. Viewer reads, Operator writes
  3. Validation rejects invalid payloads
  4. Sync status included
  5. Optimistic concurrency via resourceVersion

#### T20: Create pkg/ui/api/cluster_mappings.go - ClusterMapping CRUD
- **File:** `pkg/ui/api/cluster_mappings.go`
- **Priority:** High | **Estimate:** 0.5 day
- **Depends on:** T11, T13
- **Description:** Full CRUD. Read=Viewer, Write=Operator. Resolves referenced RemoteClusters in detail view.
- **Acceptance Criteria:**
  1. CRUD works correctly
  2. RBAC enforced
  3. Referenced clusters resolved in detail
  4. Proper validation

#### T21: Create pkg/ui/api/pvc_operations.go - PVC operation endpoints
- **File:** `pkg/ui/api/pvc_operations.go`
- **Priority:** High | **Estimate:** 0.5 day
- **Depends on:** T11, T13
- **Description:** Read-only: PVCSyncOperations and VolumeBackupOperations. Filter by namespace, status, time range. Viewer role.
- **Acceptance Criteria:**
  1. List endpoints with filtering
  2. Detail endpoints with full status
  3. Filtering works
  4. Pagination support

#### T22: Create pkg/ui/api/backup_repos.go - BackupRepository CRUD
- **File:** `pkg/ui/api/backup_repos.go`
- **Priority:** High | **Estimate:** 0.5 day
- **Depends on:** T11, T13
- **Description:** Full CRUD. Read=Viewer, Write=Operator. Credentials masked in responses.
- **Acceptance Criteria:**
  1. CRUD works correctly
  2. RBAC enforced
  3. Validation on create/update
  4. Credentials masked

#### T23: Create pkg/ui/api/users.go - User management endpoints
- **File:** `pkg/ui/api/users.go`
- **Priority:** High | **Estimate:** 1 day
- **Depends on:** T7, T11
- **Description:** Admin-only CRUD. Prevent deleting last admin. Password hashed, never returned. GET /me for any authenticated user.
- **Acceptance Criteria:**
  1. All CRUD operations work
  2. Admin-only access enforced
  3. Cannot delete last admin
  4. Password hashed on create/update
  5. Password never in responses
  6. /me works for any authenticated user

#### T24: Create pkg/ui/api/operations.go - DR operation trigger endpoints
- **File:** `pkg/ui/api/operations.go`
- **Priority:** High | **Estimate:** 1 day
- **Depends on:** T11, T13
- **Description:** POST stage/cutover/failback (async, returns 202). GET status + progress. POST cancel. Operator role. Max 1 concurrent op per NamespaceMapping.
- **Acceptance Criteria:**
  1. Trigger returns 202 with operation ID
  2. Status returns state + log
  3. Cancel stops running operation
  4. Operator role required
  5. Concurrent limit enforced

#### T25: Create pkg/ui/operations/runner.go - Async operation execution
- **File:** `pkg/ui/operations/runner.go`
- **Priority:** High | **Estimate:** 1.5 days
- **Depends on:** T13, T15
- **Description:** Async goroutine runner reusing pkg/cli/mode.go. In-memory state. WebSocket progress broadcasts. Cancellation via context.
- **Acceptance Criteria:**
  1. Operations execute asynchronously
  2. Reuses pkg/cli/mode.go logic
  3. Progress captured and stored
  4. WebSocket broadcasts work
  5. Concurrent limit enforced
  6. Cancellation works
  7. Failed operations have error details

#### T26: API tests for all endpoints with RBAC verification
- **Files:** `pkg/ui/api/*_test.go`, `pkg/ui/operations/*_test.go`
- **Priority:** High | **Estimate:** 2 days
- **Depends on:** T18-T25 (all API tasks)
- **Description:** httptest for each endpoint. Test with Viewer/Operator/Admin JWTs. CRUD, validation, errors, pagination. Mocked K8s client.
- **Acceptance Criteria:**
  1. Every endpoint tested with all three roles
  2. CRUD happy paths covered
  3. Validation error cases covered
  4. 404/409/403 cases covered
  5. >80% coverage on api and operations packages

---

### F5: React Frontend - Core

#### T27: Initialize Vite + React + TypeScript project
- **Files:** `web/ui/package.json`, `web/ui/vite.config.ts`, `web/ui/tsconfig.json`, `web/ui/index.html`
- **Priority:** High | **Estimate:** 0.5 day
- **Depends on:** T5
- **Description:** Initialize Vite project in web/ui/. Proxy to Go backend (:8443) in dev. Path aliases (@/). TypeScript strict mode.
- **Acceptance Criteria:**
  1. `npm install` succeeds
  2. `npm run dev` starts Vite dev server
  3. `npm run build` produces `web/ui/dist/`
  4. TypeScript strict mode enabled
  5. API proxy to :8443 in dev mode

#### T28: Configure shadcn/ui, Tailwind CSS, and component library
- **Files:** `web/ui/tailwind.config.js`, `web/ui/components.json`, `web/ui/src/lib/utils.ts`
- **Priority:** High | **Estimate:** 0.5 day
- **Depends on:** T27
- **Description:** Install Tailwind + shadcn/ui. Install core components (Button, Input, Card, Table, Dialog, etc.). Dark mode via class strategy.
- **Acceptance Criteria:**
  1. Tailwind works with custom config
  2. shadcn/ui components render correctly
  3. Dark mode toggle works
  4. All listed components installed

#### T29: Create src/api/client.ts - fetch wrapper with JWT auto-refresh
- **Files:** `web/ui/src/api/client.ts`, `web/ui/src/api/types.ts`
- **Priority:** High | **Estimate:** 1 day
- **Depends on:** T27
- **Description:** Fetch wrapper with JWT in Authorization header, auto-refresh on 401, redirect to login on failure. TypeScript types for all CRDs.
- **Acceptance Criteria:**
  1. All CRD types defined in TypeScript
  2. Fetch wrapper adds auth headers
  3. Auto-refresh on 401 works
  4. Redirect to login on auth failure
  5. Generic request/response typing

#### T30: Create Zustand stores for auth and preferences
- **Files:** `web/ui/src/store/auth.ts`, `web/ui/src/store/preferences.ts`
- **Priority:** High | **Estimate:** 0.5 day
- **Depends on:** T29
- **Description:** Auth store (user, tokens, login/logout). Preferences store (theme, sidebar, refresh interval). Persist to localStorage.
- **Acceptance Criteria:**
  1. Auth store manages JWT lifecycle
  2. Tokens persist in localStorage
  3. Logout clears all state
  4. Preferences persist across sessions
  5. Theme changes apply immediately

#### T31: Create useWebSocket hook with auto-reconnect
- **File:** `web/ui/src/hooks/useWebSocket.ts`
- **Priority:** High | **Estimate:** 1 day
- **Depends on:** T29, T30
- **Description:** WebSocket hook: connect with JWT, auto-reconnect (exponential backoff 1s->30s), update TanStack Query cache on CRD events, expose connection status.
- **Acceptance Criteria:**
  1. Connects with JWT auth
  2. Auto-reconnect on disconnect
  3. Exponential backoff with 30s cap
  4. TanStack Query cache updated
  5. Connection status exposed
  6. Clean disconnect on unmount

#### T32: Create App.tsx and routes.tsx with React Router v6
- **Files:** `web/ui/src/App.tsx`, `web/ui/src/routes.tsx`, `web/ui/src/main.tsx`
- **Priority:** High | **Estimate:** 0.5 day
- **Depends on:** T30
- **Description:** React Router v6 routes for all pages. App.tsx wraps with QueryClientProvider, ThemeProvider. Root redirects to /dashboard.
- **Acceptance Criteria:**
  1. All routes defined and navigable
  2. Root redirects to dashboard
  3. QueryClient provider wraps app
  4. Theme provider applies correctly
  5. Browser navigation works

#### T33: Create layout components: Sidebar, Header, ProtectedRoute
- **Files:** `web/ui/src/components/layout/Sidebar.tsx`, `Header.tsx`, `ProtectedRoute.tsx`, `AppLayout.tsx`
- **Priority:** High | **Estimate:** 1 day
- **Depends on:** T28, T30, T32
- **Description:** Collapsible sidebar, header with user info + theme toggle, ProtectedRoute with role-based access, AppLayout wrapper.
- **Acceptance Criteria:**
  1. Sidebar collapses/expands
  2. Active route highlighted
  3. Admin-only links hidden from non-admins
  4. Header shows current user
  5. ProtectedRoute redirects unauthenticated users
  6. Role-based route protection works

---

### F6: React Frontend - Pages

#### T34: Create Login page
- **File:** `web/ui/src/pages/Login.tsx`
- **Priority:** High | **Estimate:** 1 day
- **Depends on:** T29, T30, T33
- **Description:** Username/password form (React Hook Form + Zod), GitHub OAuth button. Error messages on failure, redirect on success.
- **Acceptance Criteria:**
  1. Form validates required fields
  2. Successful login redirects to dashboard
  3. Failed login shows error
  4. GitHub OAuth button works
  5. Accessible (keyboard, screen reader)

#### T35: Create Dashboard page
- **File:** `web/ui/src/pages/Dashboard.tsx`
- **Priority:** High | **Estimate:** 2 days
- **Depends on:** T31, T33, T36
- **Description:** Cluster health cards, active sync progress bars (WebSocket), recent history table, quick-action buttons, Recharts visualization. Responsive.
- **Acceptance Criteria:**
  1. Real-time cluster health cards
  2. Active syncs with progress bars
  3. Recent history with sortable columns
  4. Quick actions navigate to wizard
  5. WebSocket updates reflected immediately
  6. Responsive layout

#### T36: Create TanStack Query hooks for all resource types
- **Files:** `web/ui/src/api/hooks/useClusters.ts`, `useNamespaceMappings.ts`, etc.
- **Priority:** High | **Estimate:** 1.5 days
- **Depends on:** T29
- **Description:** TanStack Query v5 hooks for all 7 resources: useList (pagination, filters), useGet, useCreate, useUpdate, useDelete. Query invalidation on mutations. Optimistic updates.
- **Acceptance Criteria:**
  1. Hooks for all 7 resource types
  2. Pagination and filters
  3. Mutation invalidation
  4. Loading/error states exposed
  5. Type-safe

#### T37: Create Clusters list and detail pages
- **Files:** `web/ui/src/pages/Clusters.tsx`, `ClusterDetail.tsx`
- **Priority:** High | **Estimate:** 1 day
- **Depends on:** T33, T36
- **Description:** List: sortable/filterable data table. Detail: connection info, health, agent status, associated PVC ops and namespace mappings. Read-only.
- **Acceptance Criteria:**
  1. List shows all clusters with status
  2. Detail shows comprehensive info
  3. Navigation between list/detail
  4. Loading skeletons
  5. Error states handled

#### T38: Create NamespaceMapping list, detail, and form pages
- **Files:** `web/ui/src/pages/NamespaceMappings.tsx`, `NamespaceMappingDetail.tsx`, `NamespaceMappingForm.tsx`
- **Priority:** High | **Estimate:** 2 days
- **Depends on:** T33, T36
- **Description:** List with status badges. Detail with resource sync progress, PVC progress bars (WebSocket), history timeline. Create/edit form with validation.
- **Acceptance Criteria:**
  1. List shows all mappings with status
  2. Detail shows real-time sync progress
  3. PVC progress via WebSocket
  4. Create form validates fields
  5. Edit pre-populates values
  6. Delete with confirmation
  7. Operator required for writes

#### T39: Create ClusterMappings page
- **File:** `web/ui/src/pages/ClusterMappings.tsx`
- **Priority:** Medium | **Estimate:** 0.5 day
- **Depends on:** T33, T36
- **Description:** Combined list + inline CRUD. Create/edit dialogs. Associated NamespaceMapping count.
- **Acceptance Criteria:**
  1. List shows all ClusterMappings
  2. Create dialog works
  3. Edit works
  4. Delete with confirmation
  5. Shows associated mapping count

#### T40: Create PVC Operations page
- **File:** `web/ui/src/pages/PVCOperations.tsx`
- **Priority:** Medium | **Estimate:** 1 day
- **Depends on:** T33, T36, T31
- **Description:** Tabbed view: PVC Sync + Volume Backup. Data tables with status, progress, filters. Real-time WebSocket updates. Detail drawer.
- **Acceptance Criteria:**
  1. Both operation types viewable
  2. Tab switching works
  3. Filtering works
  4. Real-time progress
  5. Detail view

#### T41: Create BackupRepositories page
- **File:** `web/ui/src/pages/BackupRepos.tsx`
- **Priority:** Medium | **Estimate:** 0.5 day
- **Depends on:** T33, T36
- **Description:** List with CRUD. Type-specific create/edit form. Credentials masked. Delete confirmation.
- **Acceptance Criteria:**
  1. List shows all repos
  2. Create with type-specific fields
  3. Credentials masked
  4. Edit preserves existing credentials
  5. Delete with confirmation

#### T42: Create Operations page with wizard and live progress
- **File:** `web/ui/src/pages/Operations.tsx`
- **Priority:** High | **Estimate:** 2 days
- **Depends on:** T33, T36, T31
- **Description:** Wizard: Select mapping -> Choose op type -> Confirm -> Live progress. WebSocket log output, cancel button. History table. Operator required to trigger.
- **Acceptance Criteria:**
  1. Wizard flow works
  2. Back/next navigation
  3. Confirmation summary
  4. Live progress via WebSocket
  5. Cancel works
  6. History table
  7. Viewer sees history only
  8. Operator can trigger/cancel

#### T43: Create Users management page (Admin only)
- **File:** `web/ui/src/pages/Users.tsx`
- **Priority:** Medium | **Estimate:** 1 day
- **Depends on:** T33, T36
- **Description:** Admin-only. Data table with CRUD. Create/edit dialogs. Role selector. Prevent deleting last admin. Password fields never pre-populated.
- **Acceptance Criteria:**
  1. List shows all users
  2. Create works
  3. Edit works
  4. Last-admin protection
  5. Role changes immediate
  6. Non-admin redirected
  7. Password fields clean

#### T44: Create Settings page
- **File:** `web/ui/src/pages/Settings.tsx`
- **Priority:** Low | **Estimate:** 0.5 day
- **Depends on:** T30, T33
- **Description:** Theme selector (Light/Dark/System), auto-refresh interval, sidebar default. Persist via Zustand. Reset to defaults button.
- **Acceptance Criteria:**
  1. Theme changes apply immediately
  2. Refresh interval effective
  3. Persist across sessions
  4. Reset to defaults works

---

### F7: Helm Chart & Deployment

#### T45: Add ui: section to values.yaml
- **File:** `charts/dr-syncer/values.yaml`
- **Priority:** High | **Estimate:** 0.5 day
- **Depends on:** T26
- **Description:** Full ui: configuration section with all fields documented.
- **Acceptance Criteria:**
  1. ui: section with all fields
  2. Production-safe defaults
  3. Comments on each field
  4. `helm lint` passes

#### T46: Create Helm templates for UI deployment, service, ingress
- **Files:** `charts/dr-syncer/templates/ui-deployment.yaml`, `ui-service.yaml`, `ui-ingress.yaml`
- **Priority:** High | **Estimate:** 1 day
- **Depends on:** T45
- **Description:** Deployment (replicas, image, probes, env vars), Service (port 8443), Ingress (optional, TLS). All conditional on ui.enabled.
- **Acceptance Criteria:**
  1. Templates render with defaults
  2. Conditional on ui.enabled
  3. Probes configured
  4. Ingress optional
  5. `helm template` succeeds

#### T47: Create Helm template for UI secrets
- **File:** `charts/dr-syncer/templates/ui-secret.yaml`
- **Priority:** High | **Estimate:** 0.5 day
- **Depends on:** T45
- **Description:** JWT secret (auto-generated if not provided), default admin user. Helm lookup to avoid regenerating on upgrade.
- **Acceptance Criteria:**
  1. JWT secret auto-generated
  2. Default admin user created
  3. Secrets preserved on upgrade
  4. Conditional on ui.enabled

#### T48: Update ClusterRole RBAC for UI service account
- **File:** `charts/dr-syncer/templates/clusterrole.yaml`
- **Priority:** High | **Estimate:** 0.5 day
- **Depends on:** T46
- **Description:** RBAC for UI service account: CRD CRUD (cluster-wide) + Secrets (namespace-scoped). ServiceAccount + ClusterRoleBinding.
- **Acceptance Criteria:**
  1. Required permissions granted
  2. Least-privilege principle
  3. Secrets scoped to dr-syncer-system
  4. CRD access cluster-wide

#### T49: Add GitHub Actions workflow for UI Docker image
- **File:** `.github/workflows/build-ui.yml`
- **Priority:** Medium | **Estimate:** 1 day
- **Depends on:** T4
- **Description:** Trigger on push to main/tags. Build frontend + backend + Docker image. Push to Docker Hub. Cache node_modules and Go modules. Lint + test before build.
- **Acceptance Criteria:**
  1. Triggers on push to main and tags
  2. Build succeeds
  3. Image pushed to registry
  4. Proper tagging
  5. Build caching
  6. Tests before build

---

## Critical Path Analysis

The longest dependency chain (critical path) determines the minimum project duration:

```
T1 (0.5d) -> T2 (1d) -> T7 (1.5d) -> T9 (1d) -> T12 (1d)
-> T11 (1d) -> T18 (0.5d) -> T26 (2d)
-> T45 (0.5d) -> T46 (1d) -> T48 (0.5d)

Total critical path: ~10.5 days (minimum with full parallelization)
```

**Actual calendar time with 4 sprints: ~7 weeks**

## Parallelization Opportunities

### Sprint 1 (3 parallel tracks):
- **Track A:** T1 -> T2 -> T7 -> T9 -> T10 -> T12
- **Track B:** T3 -> T8 -> T11
- **Track C:** T6 (independent) + T4 -> T5

After T2 completes, add:
- **Track D:** T13 -> T14 -> T16 -> T17
- **Track E:** T15 (parallel with T14)

### Sprint 2 (7 parallel tracks after Sprint 1):
- T18, T19, T20, T21, T22, T23, T24, T25 (all in parallel)
- Then T26 (serial - testing)

### Sprint 3 (can overlap with Sprint 2):
- **Track A:** T27 -> T28/T29 (parallel) -> T30 -> T32 -> T33
- **Track B:** T27 -> T29 -> T36 (hooks can start early)
- After T33: T34-T44 (11 pages, fully parallelizable)

### Sprint 4:
- T45 -> T46/T47 (parallel) -> T48
- T49 (independent, can start in Sprint 1)

---

## Risk Register

| Risk | Impact | Mitigation |
|------|--------|------------|
| K8s informer complexity | Medium | T14 has detailed acceptance criteria, well-tested patterns in existing codebase |
| WebSocket reliability | Medium | T31 includes exponential backoff, T15 has ping/pong keepalive |
| JWT security | High | T8 follows industry standards (HS256, short-lived access tokens, server-side refresh) |
| Frontend build embedding | Low | Proven pattern with Go embed.FS, well-documented in design doc |
| Operation runner stability | Medium | In-memory state acceptable for v1, T25 includes cancellation and error handling |
| Helm chart correctness | Low | T46 includes helm template testing, standard patterns |

---

## File Index

All new files to be created, grouped by package:

```
cmd/ui/
  main.go                                    [T1]

pkg/ui/
  server.go                                  [T2]
  config.go                                  [T3]
  auth/
    user_store.go                            [T7]
    user_store_test.go                       [T12]
    jwt.go                                   [T8]
    jwt_test.go                              [T12]
    handler.go                               [T9]
    handler_test.go                          [T12]
    github_oauth.go                          [T10]
    github_oauth_test.go                     [T12]
    middleware.go                             [T11]
    middleware_test.go                        [T12]
  api/
    clusters.go                              [T18]
    clusters_test.go                         [T26]
    namespace_mappings.go                    [T19]
    namespace_mappings_test.go               [T26]
    cluster_mappings.go                      [T20]
    cluster_mappings_test.go                 [T26]
    pvc_operations.go                        [T21]
    pvc_operations_test.go                   [T26]
    backup_repos.go                          [T22]
    backup_repos_test.go                     [T26]
    users.go                                 [T23]
    users_test.go                            [T26]
    operations.go                            [T24]
    operations_test.go                       [T26]
  ws/
    hub.go                                   [T15]
    hub_test.go                              [T17]
    watcher.go                               [T16]
    watcher_test.go                          [T17]
  k8s/
    client.go                                [T13]
    informers.go                             [T14]
  operations/
    runner.go                                [T25]
    runner_test.go                           [T26]

web/ui/
  package.json                               [T27]
  vite.config.ts                             [T27]
  tsconfig.json                              [T27]
  tailwind.config.js                         [T28]
  postcss.config.js                          [T28]
  components.json                            [T28]
  index.html                                 [T27]
  src/
    main.tsx                                 [T32]
    App.tsx                                  [T32]
    routes.tsx                               [T32]
    api/
      client.ts                              [T29]
      types.ts                               [T29]
      hooks/
        useClusters.ts                       [T36]
        useNamespaceMappings.ts              [T36]
        useClusterMappings.ts                [T36]
        usePVCOperations.ts                  [T36]
        useBackupRepos.ts                    [T36]
        useOperations.ts                     [T36]
        useUsers.ts                          [T36]
    store/
      auth.ts                                [T30]
      preferences.ts                         [T30]
    hooks/
      useWebSocket.ts                        [T31]
    lib/
      utils.ts                               [T28]
    components/
      layout/
        Sidebar.tsx                           [T33]
        Header.tsx                            [T33]
        ProtectedRoute.tsx                    [T33]
        AppLayout.tsx                         [T33]
    pages/
      Login.tsx                              [T34]
      Dashboard.tsx                           [T35]
      Clusters.tsx                            [T37]
      ClusterDetail.tsx                       [T37]
      NamespaceMappings.tsx                   [T38]
      NamespaceMappingDetail.tsx              [T38]
      NamespaceMappingForm.tsx                [T38]
      ClusterMappings.tsx                     [T39]
      PVCOperations.tsx                       [T40]
      BackupRepos.tsx                         [T41]
      Operations.tsx                          [T42]
      Users.tsx                               [T43]
      Settings.tsx                            [T44]

build/
  Dockerfile.ui                              [T4]

charts/dr-syncer/
  values.yaml                                [T45] (modified)
  templates/
    ui-deployment.yaml                       [T46]
    ui-service.yaml                          [T46]
    ui-ingress.yaml                          [T46]
    ui-secret.yaml                           [T47]
    clusterrole.yaml                         [T48] (modified)

.github/workflows/
  build-ui.yml                               [T49]

Makefile                                     [T5] (modified)
go.mod                                       [T6] (modified)
go.sum                                       [T6] (modified)
```

### Modified Files (existing)

| File | Task | Change |
|------|------|--------|
| `Makefile` | T5 | Add build-ui, docker-build-ui, dev-ui targets |
| `go.mod` / `go.sum` | T6 | Add chi, cors, jwt, oauth2, websocket deps |
| `charts/dr-syncer/values.yaml` | T45 | Add ui: section |
| `charts/dr-syncer/templates/clusterrole.yaml` | T48 | Add UI service account RBAC |
