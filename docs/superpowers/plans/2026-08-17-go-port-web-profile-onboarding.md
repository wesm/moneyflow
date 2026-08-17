# Go Port Web Profile Routing and Onboarding Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `moneyflow web` serve a keyboard-complete profile selector and Monarch onboarding
wizard with profile-keyed URLs and APIs at any configured base path.

**Architecture:** Start one profile-neutral server with a catalog API, a lazy profile-service
registry, and profile-resolving Huma handlers. Scope mutation tokens and onboarding attempts to the
selected profile. Route one Svelte SPA between catalog and profile modes, using kit-ui components
and polling the Plan 2 coordinator for credential-blind progress.

**Tech Stack:** Go 1.26.3, Huma 2.38, `net/http`, Svelte 5, TypeScript, Vite, kit-ui, openapi-fetch,
Vitest, Playwright/Chromium, Testify

## Global Constraints

- Plans 1-3 and the approved design spec are required inputs.
- Assets remain at the configured base path; app URLs use `<base>/p/<profile-id>/`.
- Profile API URLs use `<base>/api/v1/profiles/<profile-id>/...`.
- There is no process-global selected profile; separate tabs may use separate profiles.
- Start, submit, cancel, create, and recovery are protected mutations with no browser credentials.
- Onboarding mutation tokens and attempt IDs are bound to server instance and profile ID.
- Status and Huma problem bodies never echo submitted credentials or raw errors.
- Web recovery evicts/closes the server's cached service before taking the exclusive lifecycle lock.
- Use kit-ui components and keep every flow keyboard reachable.
- Use TDD and Testify/Vitest/Playwright. Commit each verified task without amending earlier commits.
- Run web and Go tests only against explicit temporary catalog roots.

---

### Task 1: Add Profile Route Parsing and Scoped Mutation Tokens

**Files:**

- Create: `internal/api/profilepath.go`
- Create: `internal/api/profilepath_test.go`
- Modify: `internal/api/security.go`
- Modify: `internal/api/security_test.go`
- Modify: `internal/api/bootstrap.go`
- Modify: `internal/api/bootstrap_test.go`
- Modify: `internal/api/server.go`

**Interfaces:**

- Produces: `api.ProfileAPIPath(basePath, profileID, endpoint string) (string, error)`
- Produces: strict path extraction for canonical profile IDs
- Produces: `MutationSecurity.Issue(scope string)` and `Verify(value, scope string)`

- [ ] **Step 1: Write failing path and token-scope tests**

```go
func TestProfileAPIPathPreservesBasePathAndCanonicalID(t *testing.T) {
    got, err := ProfileAPIPath("/moneyflow/", testProfileID, "view")
    require.NoError(t, err)
    assert.Equal(t, "/moneyflow/api/v1/profiles/"+testProfileID+"/view", got)
}

func TestMutationTokenCannotCrossProfiles(t *testing.T) {
    security := newTestSecurity(t)
    issued, err := security.Issue(testProfileID)
    require.NoError(t, err)
    require.NoError(t, security.Verify(issued.Value, testProfileID))
    assert.ErrorIs(t, security.Verify(issued.Value, otherProfileID), ErrInvalidMutationToken)
}
```

Cover encoded slashes/dots, invalid IDs, base path `/`, catalog scope, token refresh/expiry,
server-instance restart, and unchanged Origin/Fetch Metadata behavior.

- [ ] **Step 2: Run API security tests and verify RED**

```bash
go test ./internal/api -run 'Test(ProfileAPIPath|MutationToken|Bootstrap)' -count=1
```

Expected: FAIL because tokens are not scoped and profile routes are absent.

- [ ] **Step 3: Add an exact scope claim and route helper**

Add `Scope string` to signed claims and require exact equality during verification. Use the fixed
scope `catalog` for the catalog bootstrap and the canonical profile ID for profile bootstrap.
Change security middleware to receive the expected scope after strict route parsing; never trust a
decoded path segment before canonical validation.

The no-store index response issues a catalog token at the base route and a profile token at a
canonical profile route. `GET /api/v1/bootstrap` refreshes only catalog tokens;
`GET /api/v1/profiles/{profile_id}/bootstrap` refreshes only that profile's token.

Keep the existing one-hour token lifetime and single token-expiry retry contract. Update bootstrap
wire data only as required to issue the correct in-memory token; do not place scope/token in URLs.

- [ ] **Step 4: Run all API security tests and verify GREEN**

```bash
go test ./internal/api -run 'Test(ProfileAPIPath|Mutation|Origin|Fetch|Bootstrap|Security)' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit profile-scoped security**

```bash
git add internal/api/profilepath.go internal/api/profilepath_test.go internal/api/security.go \
  internal/api/security_test.go internal/api/bootstrap.go internal/api/bootstrap_test.go \
  internal/api/server.go
git commit -m "feat: scope web mutations to profiles"
```

### Task 2: Add a Lazy Profile Service Registry and Profile-Resolving API

**Files:**

- Create: `internal/web/profiles.go`
- Create: `internal/web/profiles_test.go`
- Create: `internal/api/profiles.go`
- Create: `internal/api/profiles_test.go`
- Modify: `internal/api/server.go`
- Modify: all `internal/api/*_test.go` helpers that construct `api.Config`
- Modify: `internal/web/server.go`
- Modify: `internal/web/server_test.go`

**Interfaces:**

- Produces: `web.ProfileRegistry.Acquire(context.Context, string) (ProfileLease, error)`
- Produces: `web.ProfileRegistry.Evict(context.Context, string) error`
- Produces: `api.ProfileResolver` used by every existing profile endpoint
- Produces: `web.ProfileRegistry.OnboardingOpener()` for configuring the cached live service

- [ ] **Step 1: Write failing registry and two-profile API tests**

```go
func TestProfileRegistryCachesByIDAndClosesAfterIdle(t *testing.T) {
    clock := newFakeClock()
    registry := newTestRegistry(t, clock)
    first, err := registry.Acquire(context.Background(), profileA)
    require.NoError(t, err)
    first.Release()
    second, err := registry.Acquire(context.Background(), profileA)
    require.NoError(t, err)
    assert.Equal(t, 1, registry.opens(profileA))
    second.Release()
    clock.Advance(profileIdleTimeout)
    registry.CloseIdle(context.Background())
    assert.Equal(t, 1, registry.closes(profileA))
}

func TestProfileAPIRoutesResolveIndependentServices(t *testing.T) {
    server := newTwoProfileAPIServer(t)
    a := postView(t, server, profileA)
    b := postView(t, server, profileB)
    assert.NotEqual(t, a.Rows[0].ID, b.Rows[0].ID)
}
```

Cover concurrent acquire/reference counts, unknown ID, close error, shutdown, Evict waiting for
active leases, Evict closing cached service, and no close while a request uses the service.

- [ ] **Step 2: Run registry/API tests and verify RED**

```bash
go test ./internal/web ./internal/api -run 'Test(ProfileRegistry|ProfileAPI)' -count=1
```

Expected: FAIL because servers are bound to one service.

- [ ] **Step 3: Refactor existing API operations through a resolver**

Define:

```go
type ProfileLease interface {
    Service() *app.Service
    Release() error
}

type ProfileResolver interface {
    Acquire(context.Context, string) (ProfileLease, error)
}
```

Change profile operation paths to include `{profile_id}` and acquire/release a lease inside every
handler before service use. Keep catalog/bootstrap endpoints service-free. Update OpenAPI to show
one templated profile ID parameter rather than one route per discovered profile.

`web.ProfileRegistry` opens through the Plan 1 catalog and Plan 2 runtime configuration, caches by
canonical ID, reference-counts requests, closes after a fixed 15-minute idle period, and closes all
services on shutdown. `Evict` prevents new acquires, waits for active leases with context, closes,
then removes the entry.

`OnboardingOpener` adapts an acquired registry lease to Plan 2's `OpenedProfile`: its close
function releases the lease, not the cached service. The web coordinator therefore imports and
installs the provider runtime on the exact `*app.Service` used by finance requests. After complete,
the API takes the result once and releases that lease; the configured cached service remains live.
Do not open a second service for web onboarding.

- [ ] **Step 4: Run API/web and editing integration tests and verify GREEN**

```bash
go test ./internal/api ./internal/web -count=1
MONEYFLOW_SKIP_PERF=1 go test ./internal/api -run 'Test.*(Editing|Mutation|Provider|Projection)' -count=1
```

Expected: PASS after test helpers use a resolver with one named profile.

- [ ] **Step 5: Commit the multi-profile service boundary**

```bash
git add internal/web/profiles.go internal/web/profiles_test.go internal/api/profiles.go \
  internal/api/profiles_test.go internal/api/server.go internal/api/*_test.go \
  internal/web/server.go internal/web/server_test.go
git commit -m "feat: resolve web services by profile"
```

### Task 3: Expose Catalog, Recovery, and Onboarding HTTP Endpoints

**Files:**

- Create: `internal/api/profilecatalog.go`
- Create: `internal/api/profilecatalog_test.go`
- Create: `internal/api/onboarding.go`
- Create: `internal/api/onboarding_test.go`
- Modify: `internal/api/errors.go`
- Modify: `internal/api/errors_test.go`
- Modify: `internal/api/server.go`
- Modify: `internal/web/server.go`

**Interfaces:**

- Consumes: Plan 1 catalog, Plan 2 coordinator, and `ProfileRegistry.Evict`
- Produces: catalog list/create/recovery APIs and exact start/submit/cancel/status endpoints

- [ ] **Step 1: Write failing endpoint/security/privacy tests**

```go
func TestOnboardingStatusIsCredentialBlind(t *testing.T) {
    server := newOnboardingAPIServer(t)
    attempt := startOnboarding(t, server, profileA)
    submitCredentials(t, server, attempt, syntheticSecrets())
    response := getOnboardingStatus(t, server, attempt)
    for _, secret := range syntheticSecrets().allStrings() {
        assert.NotContains(t, response.Body.String(), secret)
    }
}

func TestRecoveryEvictsCachedServiceBeforeExclusiveLifecycleLock(t *testing.T) {
    registry, catalog, server := recoveryServerWithCachedProfile(t)
    confirmRecovery(t, server, profileA)
    assert.Equal(t, []string{"evict", "recreate"}, orderedCalls(registry, catalog))
}
```

Cover catalog bootstrap/list, create, local status, newer refusal, profile-bound token mismatch,
stale state version, wrong attempt/profile, cancel, expiry, body limits, invalid Origin,
Sec-Fetch-Site, raw-provider-error redaction, and Huma problems excluding request bodies.

- [ ] **Step 2: Run endpoint tests and verify RED**

```bash
go test ./internal/api ./internal/web -run 'Test(Onboarding|ProfileCatalog|Recovery)' -count=1
```

Expected: FAIL because endpoints do not exist.

- [ ] **Step 3: Register exact bounded wire contracts**

Register:

```text
GET  /api/v1/bootstrap
GET  /api/v1/profiles
POST /api/v1/profiles
POST /api/v1/profiles/{profile_id}/recovery
GET  /api/v1/profiles/{profile_id}/bootstrap
POST /api/v1/profiles/{profile_id}/onboarding/start
POST /api/v1/profiles/{profile_id}/onboarding/{attempt_id}/submit
POST /api/v1/profiles/{profile_id}/onboarding/{attempt_id}/cancel
GET  /api/v1/profiles/{profile_id}/onboarding/{attempt_id}/status
```

Use exact version-one action unions and status fields from the spec. Apply catalog-scope tokens to
catalog create/recovery and profile-scope tokens to onboarding. Recovery must call Evict before
catalog Recreate. Construct the coordinator with `ProfileRegistry.OnboardingOpener`; on completion,
take the opened profile exactly once and release its lease so the registry remains its owner. Map
every catalog/onboarding code exhaustively to sanitized Huma problems.

- [ ] **Step 4: Generate/check OpenAPI and verify endpoint tests GREEN**

```bash
go test ./internal/api ./internal/web -run 'Test(Onboarding|ProfileCatalog|Recovery|OpenAPI)' -count=1
make web-generate
make web-check
```

Expected: generated TypeScript includes all catalog/onboarding schemas and routes; tests pass.

- [ ] **Step 5: Commit the HTTP contract and generated types**

```bash
git add internal/api/profilecatalog.go internal/api/profilecatalog_test.go \
  internal/api/onboarding.go internal/api/onboarding_test.go internal/api/errors.go \
  internal/api/errors_test.go internal/api/server.go internal/web/server.go \
  web/src/lib/api/schema.d.ts
git commit -m "feat: expose profile onboarding API"
```

### Task 4: Make Web Startup Profile-Neutral and Route the SPA by Profile ID

**Files:**

- Create: `web/src/lib/controller/routing.ts`
- Create: `web/src/lib/controller/routing.test.ts`
- Create: `web/src/lib/api/catalog-client.ts`
- Create: `web/src/lib/api/catalog-client.test.ts`
- Modify: `web/src/lib/api/client.ts`
- Modify: `web/src/lib/api/client.test.ts`
- Modify: `web/src/lib/controller/base-path.ts`
- Modify: `web/src/lib/controller/base-path.test.ts`
- Modify: `web/src/main.ts`
- Modify: `cmd/moneyflow/web.go`
- Modify: `cmd/moneyflow/web_test.go`
- Modify: `internal/web/handler.go`
- Modify: `internal/web/handler_test.go`

**Interfaces:**

- Produces: `parseApplicationRoute(basePath, location) -> catalog | profile`
- Produces: catalog client and profile-scoped `MoneyflowClient`
- Produces: profile-neutral `WebRunner` and preselection redirect

- [ ] **Step 1: Write failing route/client/startup tests**

```ts
it('parses a profile route without consuming analytical query state', () => {
  const route = parseApplicationRoute('/moneyflow/',
    new URL('https://example.test/moneyflow/p/profile_aaaaaaaaaaaaaaaaaaaaaaaaaa/?v=1&group=merchant'))
  expect(route).toEqual({ kind: 'profile', profileID: 'profile_aaaaaaaaaaaaaaaaaaaaaaaaaa' })
})

it('builds profile API paths while keeping assets at the base path', () => {
  expect(profileAPIBase('/moneyflow/', profileID)).toBe(
    `/moneyflow/api/v1/profiles/${profileID}/`,
  )
  expect(assetBase('/moneyflow/')).toBe('/moneyflow/')
})
```

Go command tests assert ordinary web startup does not open a profile, `--profile` redirects to the
canonical ID route, `--demo` receives a process-local route, and browser-open URL follows the
redirect model.

- [ ] **Step 2: Run route and command tests and verify RED**

```bash
bun run --cwd web test -- src/lib/controller/routing.test.ts \
  src/lib/api/catalog-client.test.ts src/lib/api/client.test.ts
go test ./cmd/moneyflow ./internal/web -run 'Test(Web|Handler)' -count=1
```

Expected: FAIL because startup and clients are single-profile.

- [ ] **Step 3: Implement one SPA routing model**

Keep static assets/index fallback at base path. Accept navigation only for base and canonical
`p/<profile-id>/` paths. `main.ts` reads base path and route separately. Catalog client refreshes a
catalog token; profile client refreshes from the selected profile bootstrap and sends only that
profile-scoped token.

Change `WebRunner` to receive catalog/registry/coordinator dependencies. Ordinary startup opens no
profile. `--profile` resolves name/ID before listening and redirects base to its canonical route;
`--demo` registers one process-local canonical-format random ID, never writes it to the catalog,
and uses the same route shape.

- [ ] **Step 4: Run TypeScript, Go, and asset checks and verify GREEN**

```bash
bun run --cwd web test -- src/lib/controller/routing.test.ts \
  src/lib/api/catalog-client.test.ts src/lib/api/client.test.ts
go test ./cmd/moneyflow ./internal/web -run 'Test(Web|Handler)' -count=1
make web-check
```

Expected: PASS.

- [ ] **Step 5: Commit routing and startup**

```bash
git add web/src/lib/controller/routing.ts web/src/lib/controller/routing.test.ts \
  web/src/lib/api/catalog-client.ts web/src/lib/api/catalog-client.test.ts \
  web/src/lib/api/client.ts web/src/lib/api/client.test.ts \
  web/src/lib/controller/base-path.ts web/src/lib/controller/base-path.test.ts web/src/main.ts \
  cmd/moneyflow/web.go cmd/moneyflow/web_test.go internal/web/handler.go \
  internal/web/handler_test.go
git commit -m "feat: route web profiles by stable ID"
```

### Task 5: Build the kit-ui Profile Selector and Onboarding Wizard

**Files:**

- Create: `web/src/components/profiles/ProfileSelector.svelte`
- Create: `web/src/components/profiles/ProfileSelector.test.ts`
- Create: `web/src/components/profiles/ProviderSelector.svelte`
- Create: `web/src/components/profiles/ProviderSelector.test.ts`
- Create: `web/src/components/profiles/ProfileNameForm.svelte`
- Create: `web/src/components/profiles/RecoveryPanel.svelte`
- Create: `web/src/components/profiles/OnboardingWizard.svelte`
- Create: `web/src/components/profiles/OnboardingWizard.test.ts`
- Create: `web/src/lib/controller/catalog.svelte.ts`
- Create: `web/src/lib/controller/catalog.test.ts`
- Create: `web/src/lib/controller/onboarding.svelte.ts`
- Create: `web/src/lib/controller/onboarding.test.ts`
- Modify: `web/src/App.svelte`
- Modify: `web/src/App.test.ts`
- Modify: `web/src/app.css`

**Interfaces:**

- Consumes: generated catalog/onboarding wire types and profile route controller
- Produces: keyboard-complete catalog, recovery, setup, progress, retry, and finance handoff

- [ ] **Step 1: Write failing component/controller tests**

```ts
it('supports Python selector keys and focusable unavailable providers', async () => {
  render(ProfileSelector, { props: syntheticCatalogProps() })
  await fireEvent.keyDown(window, { key: 'a' })
  expect(screen.getByRole('heading', { name: 'Choose a provider' })).toBeVisible()
  await fireEvent.keyDown(window, { key: 'y' })
  expect(screen.getByRole('status')).toHaveTextContent('YNAB is not available in Go yet.')
})

it('uses password controls and clears secrets after submit', async () => {
  render(OnboardingWizard, { props: credentialRequiredProps() })
  const password = screen.getByLabelText('Monarch password')
  expect(password).toHaveAttribute('type', 'password')
  await userEvent.type(password, 'synthetic-secret')
  await userEvent.click(screen.getByRole('button', { name: 'Connect' }))
  expect(password).toHaveValue('')
})
```

Cover alphabetical statuses, d/a/n/Escape/q, arrows/Home/Enter, local-only Open Offline, newer
without Recreate, explicit recovery, provider m/y/s, USD/2 confirmation, unlock, credential
validation, progress counts/elapsed, cancel, retry vs reauth, identity mismatch, announcements,
and success navigation preserving query state.

- [ ] **Step 2: Run frontend tests and verify RED**

```bash
bun run --cwd web test -- src/components/profiles \
  src/lib/controller/catalog.test.ts src/lib/controller/onboarding.test.ts src/App.test.ts
```

Expected: FAIL because the profile UI does not exist.

- [ ] **Step 3: Implement controllers and kit-ui views**

Use kit-ui `TopBar`, `Button`, `StatusBar`, form primitives, focus styles, and shortcut manager.
Keep secret strings only in component-local password inputs; clear immediately before sending and
after every failure. Controllers store only coordinator snapshots. Poll only while a job runs or a
tab is visible, and treat successful status polls as attempt activity.

`App.svelte` renders catalog mode at base and the existing finance `AppShell` in profile mode.
Reconnect from `ProviderStatus` opens the same `OnboardingWizard` over the preserved profile
controller, then rechecks without losing analytical URL/history state.

- [ ] **Step 4: Run component, accessibility, and kit-ui usage checks and verify GREEN**

```bash
bun run --cwd web test -- src/components/profiles \
  src/lib/controller/catalog.test.ts src/lib/controller/onboarding.test.ts src/App.test.ts
make web-check
```

Expected: tests, Svelte/TypeScript checks, and `kit-ui-check` pass.

- [ ] **Step 5: Commit the browser experience**

```bash
git add web/src/components/profiles web/src/lib/controller/catalog.svelte.ts \
  web/src/lib/controller/catalog.test.ts web/src/lib/controller/onboarding.svelte.ts \
  web/src/lib/controller/onboarding.test.ts web/src/App.svelte web/src/App.test.ts web/src/app.css
git commit -m "feat: add web profile onboarding"
```

### Task 6: Add Real Browser Security and Keyboard Journeys

**Files:**

- Create: `internal/tools/webtestserver/main.go`
- Create: `web/tests/onboarding.spec.ts`
- Create: `web/tests/profile-routing.spec.ts`
- Create: `web/tests/onboarding-security.spec.ts`
- Modify: `web/scripts/e2e-server.ts`
- Modify: `web/tests/provider.spec.ts`
- Modify: `web/tests/restart.spec.ts`
- Modify: `web/tests/editing.spec.ts`

**Interfaces:**

- Consumes: injectable web catalog/coordinator/profile registry
- Produces: a test-only process with synthetic provider runtime and no production escape hatch

- [ ] **Step 1: Write failing Playwright journeys**

Add journeys that:

- create a named synthetic Monarch profile entirely by keyboard;
- enter password fields, observe phases/counts, and land on the finance route;
- open two profiles in separate tabs and prove independent data/revisions;
- reject cross-profile token and attempt reuse;
- confirm secret values are absent from DOM after submit, network responses, problems, console,
  and server logs;
- evict a cached profile before recovery;
- route session expiry to reconnect without losing URL/cursor state; and
- follow `--demo`/`--profile` base redirects before existing editing/restart flows.

- [ ] **Step 2: Run focused E2E and verify RED**

```bash
make web-build
bun run --cwd web test:e2e -- tests/onboarding.spec.ts tests/profile-routing.spec.ts \
  tests/onboarding-security.spec.ts --project=chromium
```

Expected: FAIL because the synthetic onboarding server/journeys are absent or incomplete.

- [ ] **Step 3: Add a test-only composed server and update journeys**

`internal/tools/webtestserver` constructs production `internal/web` handlers with temp catalog,
fake connector/session/source, injected clock, and synthetic transactions. It must not be imported
by production commands and must refuse a non-temp profile root. Extend `e2e-server.ts` to build/run
that tool only for onboarding scenarios.

Update existing `--demo` and persistent-profile journeys to wait for the canonical profile-route
redirect. Do not add environment-controlled fake-provider code to the production binary.

- [ ] **Step 4: Run browser, audit, and editing gates and verify GREEN**

```bash
make web-e2e
make test-editing-e2e
make web-audit
```

Expected: PASS with no accessibility, console, request, or dependency-audit failures.

- [ ] **Step 5: Commit browser proof**

```bash
git add internal/tools/webtestserver/main.go web/tests/onboarding.spec.ts \
  web/tests/profile-routing.spec.ts web/tests/onboarding-security.spec.ts \
  web/scripts/e2e-server.ts web/tests/provider.spec.ts web/tests/restart.spec.ts \
  web/tests/editing.spec.ts
git commit -m "test: prove web onboarding journeys"
```

### Task 7: Embed Assets, Verify Tailnet/Base-Path Behavior, and Complete Plan 4

**Files:**

- Modify: `internal/web/dist/**` through deliberate generation/embed commands
- Modify: `README.md`
- Modify: `Makefile` only if stable target composition changes
- Modify: `cmd/moneyflow/web_test.go`

**Interfaces:**

- Produces: committed production assets and final profile-neutral web command

- [ ] **Step 1: Add final command/base-path assertions**

Test canonical external URL `https://host-a.example/moneyflow/`, assets under `/moneyflow/assets/`,
selector at `/moneyflow/`, selected app at `/moneyflow/p/<id>/`, profile APIs under the exact base,
and direct listener mutation refusal when canonical origin differs.

- [ ] **Step 2: Run focused command/handler tests and verify RED if coverage is missing**

```bash
go test ./cmd/moneyflow ./internal/web ./internal/api -run 'Test.*(BasePath|ExternalURL|Profile|Origin)' -count=1
```

Expected: any missing contract assertion fails before the final implementation adjustment.

- [ ] **Step 3: Generate and embed reviewed production assets**

Run deliberate write operations:

```bash
make web-generate
make web-build
make web-embed
git diff -- internal/web/dist web/src/lib/api/schema.d.ts
```

Inspect the manifest/assets and generated schema diff. Confirm no source maps, secrets, personal
data, absolute local paths, or unexpected assets are present.

- [ ] **Step 4: Run the complete repository verification**

```bash
PLAN4_VERIFY_HOME="$(mktemp -d)"
MONEYFLOW_HOME="$PLAN4_VERIFY_HOME" MONEYFLOW_SKIP_PERF=1 make verify-go
MONEYFLOW_HOME="$PLAN4_VERIFY_HOME" make verify-web
MONEYFLOW_HOME="$PLAN4_VERIFY_HOME" uv run pytest -v
uv run pyright moneyflow/
uv run ruff format --check moneyflow/ tests/
uv run ruff check moneyflow/ tests/
npx --yes markdownlint-cli@0.47.0 --config .markdownlint.json README.md 'docs/**/*.md'
.github/scripts/check-arrow-lists.sh
```

Expected: every Go, Python, frontend, generated-asset, security, parity, and Chromium gate passes.
Confirm the final diff contains no migration, provider write-back, YNAB/SimpleFIN adapter, general
profile rename/delete/import, or production fake-provider path.

- [ ] **Step 5: Commit the production web assets and documentation**

```bash
git add internal/web/dist web/src/lib/api/schema.d.ts README.md Makefile \
  cmd/moneyflow/web_test.go
git commit -m "feat: ship profile-aware web onboarding"
```

Verify `git status --short` is empty. Only then dogfood live Monarch through TUI and web over the
configured tailnet/base path. Any correction discovered manually is a new tested commit.
