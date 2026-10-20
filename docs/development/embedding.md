# Embedding Moneyflow

A Go application can run Moneyflow in its own process. It opens a Moneyflow instance, serves
Moneyflow's API under a path it chooses, adds Moneyflow's MCP tools to its own MCP server, and
shows Moneyflow's screens with the `moneyflow-views` Svelte package. This page calls that
application the host.

The host does four jobs that standalone Moneyflow does for itself:

- **Admission.** The embedded API checks no origin and no token. The host decides who may call it
  and authorizes every `commit` operation itself.
- **Refresh schedule.** By default the views refresh provider data only when the user asks. The
  host runs `RunScheduler`.
- **Recovery points.** The host calls `Backup` when it wants one.
- **Navigation.** The host decides where each profile's workspace and setup appear.

## Open an instance

Import `github.com/wesm/moneyflow` as package `moneyflow`. The package does not need Moneyflow's
built web assets, its terminal UI, or its CLI. The Go examples on this page form one file.

```go
package host

import (
    "context"
    "log/slog"
    "net/http"

    "github.com/modelcontextprotocol/go-sdk/mcp"

    moneyflow "github.com/wesm/moneyflow"
)

// APIBase is where the host serves Moneyflow's API.
const APIBase = "/embedded/moneyflow/"

// Start opens Moneyflow, mounts its API on mux, and refreshes provider data until ctx ends.
func Start(
    ctx context.Context, mux *http.ServeMux, root string, allowCommit func(*http.Request) bool,
) (*moneyflow.Instance, error) {
    instance, err := moneyflow.Open(ctx, moneyflow.Options{
        Root:        root,
        APIBasePath: APIBase,
        Logger:      slog.Default(),
    })
    if err != nil {
        return nil, err
    }
    mux.Handle(APIBase, Admit(instance, allowCommit))
    go func() {
        slog.Info("Moneyflow refresh schedule stopped", "reason", instance.RunScheduler(ctx))
    }()
    return instance, nil
}
```

- `Root` is an absolute data directory laid out like `MONEYFLOW_HOME`. Each profile lives in
  `Root/profiles/<profile ID>`. `Open` creates `Root` with private permissions. Every ancestor
  must be owned by the user or root and must not be group- or world-writable.
- `APIBasePath` is a clean path that begins and ends with `/`.
- `Logger` receives background failures, the API's unexpected errors, and every recovered panic
  with its stack, so treat its output as sensitive. A nil `Logger` discards them.
- `Open` opens no profile and succeeds while a profile is broken. That profile reports `error`,
  and the others keep working.
- Open one instance per `Root`. `Close` stops background work, waits up to 30 seconds for it, and
  closes every profile. After `Close`, every new API request answers 503 `profile_busy`, and every
  method that returns an error returns `ErrClosed`.
- Standalone Moneyflow's terminal UI, web UI, and MCP server take shared profile locks, so they
  can use the same data directory. A profile that another process holds exclusively, for example
  during an import or a recovery, reports `error` until that process finishes.

## Admit API requests

`Instance.API()` serves every operation. Mount it without stripping the path prefix. Every
operation has one class:

| Class | Meaning | Host action |
| --- | --- | --- |
| `read` | Writes nothing | Forward it |
| `stage` | Changes only local data or state: pending edits, refreshed provider data, exports, setup, reconnecting, and imports | Forward it unless the host is read-only |
| `commit` | Sends edits to a provider or confirms a destructive change, such as removing a profile | Forward it only after the host's own check, such as a confirmation or a permission |

`Instance.Operations()` lists every route with its class. Each `Method + " " + Path` is a valid
`http.ServeMux` pattern, so a private mux finds the class of a request:

```go
// operationClass is a handler that does nothing; it carries one operation's class.
type operationClass moneyflow.Class

func (operationClass) ServeHTTP(http.ResponseWriter, *http.Request) {}

// Admit forwards read and stage operations, and commit operations that allowCommit accepts.
func Admit(instance *moneyflow.Instance, allowCommit func(*http.Request) bool) http.Handler {
    classes := http.NewServeMux()
    for _, operation := range instance.Operations() {
        classes.Handle(operation.Method+" "+operation.Path, operationClass(operation.Class))
    }
    api := instance.API()
    return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
        matched, _ := classes.Handler(request)
        class, known := matched.(operationClass)
        if !known || (moneyflow.Class(class) == moneyflow.ClassCommit && !allowCommit(request)) {
            http.Error(writer, "Forbidden", http.StatusForbidden)
            return
        }
        api.ServeHTTP(writer, request)
    })
}
```

In read-only mode, answer every operation that is not `read` with 403 and pass `readOnly` to the
views. Errors are [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) problem JSON with a `code` and
a `detail`. A route for a profile that another process holds answers 503 `profile_busy`; retry it
later. A route for a profile that cannot open for another reason answers 404 `not_found`;
`Profiles` says why.

The host calls a few operations itself. `<base>` stands for `APIBasePath`:

- `GET <base>api/v1/profiles` (read) lists the profiles.
- `POST <base>api/v1/profiles` (stage) with
  `{"version":"1","display_name":"Example Budget","provider_kind":"monarch"}` creates a profile in
  `needs_setup`. `provider_kind` is `monarch`, `ynab`, `simplefin`, `amazon`, or `local`.
- `POST <base>api/v1/profiles/{profile_id}/remove` (commit) with `{"version":"1"}` deletes the
  profile and its files. It answers 503 `profile_busy` while any process holds the profile.
- `GET <base>openapi.json` and `GET <base>openapi.yaml` (read) describe every operation.

## Show each profile by its state

`Instance.Profiles(ctx)` returns every profile in `Root/profiles`, sorted by name. Each `Profile`
has an `ID`, `Name`, `ProviderKind`, `State`, `Error`, `LastRefresh` (zero before the first
refresh), and `PendingEdits`. Show each profile by its state:

| State | Meaning | Host action |
| --- | --- | --- |
| `ready` | Nothing needs the user | Show `MoneyflowWorkspace` |
| `needs_setup` | Setup never finished | Show `MoneyflowSetup` with `provider` and `profileID` |
| `needs_reconnect` | Provider sign-in is missing or expired, as after a restore | Show the workspace; its `onreconnect` opens `MoneyflowSetup` with `profileID` |
| `needs_unlock` | YNAB credentials are stored but locked | Call `Unlock`, or show `MoneyflowSetup` with `profileID` |
| `refresh_failed` | The last refresh failed; `Error` says why | Show the workspace |
| `write_attention` | A commit to the provider stopped and waits for the user, for example because it is paused or needs a decision | Show the workspace; its review drawer shows what the commit needs |
| `error` | The profile cannot open; `Error` says why | Show `Error`, not the workspace |

`Profiles` ignores a legacy database at the top of `Root`. `ImportProfiles` gives such a profile an
ID.

## Add the MCP tools

Build a new `*mcp.Server` for each MCP request and call `RegisterTools` on it. `mcp` is the official
MCP Go SDK, `github.com/modelcontextprotocol/go-sdk/mcp`.

```go
// MCPServer returns an MCP server with Moneyflow's read and stage tools.
func MCPServer(instance *moneyflow.Instance) (*mcp.Server, error) {
    server := mcp.NewServer(&mcp.Implementation{Name: "example-host", Version: "1"}, nil)
    err := instance.RegisterTools(server, moneyflow.ToolOptions{
        Classes: []moneyflow.Class{moneyflow.ClassRead, moneyflow.ClassStage},
        Prefix:  "moneyflow_",
    })
    return server, err
}
```

- `Classes` lists the classes of the tools to add. Tools have the same three classes as API
  operations. The [MCP guide](../guide/mcp.md) describes each tool.
- `Prefix` goes in front of every tool name. It may contain letters, digits, `_`, `.`, and `-`.
- With `ClassRead`, `list_profiles` lists each profile's `id`, `name`, `provider`, and `state`.
- Every other tool takes an optional `profile` argument, a profile ID. It defaults to the only
  profile. With several profiles, a call without it fails with `profile_required`.
- A call on a profile that cannot open fails with `profile_unavailable`. Its `detail` gives the
  reason, such as `profile_busy: …` while another process holds the profile.
- `RegisterTools` adds no resources or middleware. It returns an error for a nil server, an empty
  class list, an unknown class, an invalid prefix, or a prefix that makes a tool name longer than
  128 characters. After `Close`, it returns `ErrClosed`.
- Each profile has one refresh at a time, which the API, the scheduler, and every MCP server
  share. The API, or a tool on any MCP server, can confirm a refresh that any of them started.

## Refresh provider data

`RunScheduler` checks every Monarch, YNAB, and SimpleFIN profile at once and then every minute, and
starts a refresh when one is due. Monarch refreshes the last 90 days every 6 hours and everything
every 30 days. YNAB refreshes every 6 hours and SimpleFIN every 24 hours. `RunScheduler` returns
`ctx.Err()` when `ctx` ends and `ErrClosed` when the instance closes. A panic in background work
puts that profile in state `error` instead of stopping the host.

The API, the scheduler, and every MCP server share each profile's refresh. `refreshProvider`
starts the profile's refresh, or joins the one already under way, and answers when it stops. A
refresh that would delete transactions waits for a confirmation: `refreshProvider` answers 409
with the refresh's confirmation token, and the views ask the user and confirm it through
`confirmProviderRefresh` with that token. An MCP tool can confirm it too. The token works for 10
minutes from the start of its refresh. A scheduled refresh that waits for a confirmation pauses
that profile's schedule until the deletion is confirmed or a later manual refresh succeeds.

## Unlock, reconnect, and import

- `Unlock(ctx, profileID, password)` unlocks a YNAB profile's stored credentials with a password
  that the host supplies. Moneyflow never stores the password. It returns `ErrUnlockFailed` for a
  wrong password, `ErrNotLocked` for a profile that is not a connected YNAB profile with stored
  credentials, and `ErrProfileNotFound` for an ID that names no profile.
- To reconnect any provider, show `MoneyflowSetup` with the profile's `profileID`. The new sign-in
  must match the profile's provider account. The profile keeps its ID, data, and pending edits.
- `ImportProfiles(ctx, sourceRoot)` copies every profile of a standalone data directory into the
  instance and reports what it did. It never changes the source's profiles or data. Like Moneyflow
  whenever it opens a data directory, it may add missing lock files to the source and restrict the
  source's files to their owner. It skips a profile whose ID already exists (`exists`), a profile
  that another process holds (`in_use`), a profile it cannot read (`unsupported`), and a profile
  whose name is taken (`name_taken`).

## Back up and restore

- `Backup(ctx, destination)` writes a data directory at the absolute path `destination`, which
  must not exist. Every ancestor of `destination` must pass the same checks as those of `Root`.
  The backup holds each profile's metadata, database, audit log, and refresh state. It leaves out
  provider credentials, sessions, and exports.
- `Backup` skips a profile it cannot open or copy, copies the others, and then returns an error
  that names each skipped profile. When `Backup` returns an error and `Verify` passes on
  `destination`, the backup holds every profile that the error does not name, so keep it.
- A legacy database at the top of `Root` is not a profile, so `Backup` leaves it out without an
  error. `ImportProfiles` gives it an ID.
- `moneyflow.Verify(ctx, dir)` checks a backup. It changes no file's contents and takes no locks,
  so verify a backup that nothing else is using.
- To restore, close the instance, put the backup directory at `Root`, and open it again. Each
  provider profile then reports `needs_reconnect`.

## Install the views

Install the views from the same Moneyflow tag or commit as the Go module, so the views match the
API they call. The repository's `package.json` makes the repository itself the `moneyflow-views`
package, and GitHub serves every tag and commit as a tarball. kit-ui is not on the npm registry, so
install it from Git in the same command:

```bash
go get github.com/wesm/moneyflow@REF
npm install "github:kenn-io/kit-ui#KIT_UI_COMMIT" \
  https://codeload.github.com/wesm/moneyflow/tar.gz/REF
```

`REF` is a tag, such as `vX.Y.Z`, or a full commit hash. `KIT_UI_COMMIT` is the kit-ui commit that
`web/package.json` pins at `REF`. npm records the tarball's URL and `integrity` hash in
`package-lock.json`. It unpacks the whole repository into `node_modules/moneyflow-views`.

The package ships Svelte 5 and TypeScript source. It uses the host's `svelte`, `@lucide/svelte`,
and kit-ui, at the ranges in its `peerDependencies`, so it follows the host's kit theme, and the
host, kit-ui, and the views share one copy of the icons. Svelte compiles the views' TypeScript
itself, so the host needs no preprocessor:

```ts
import { svelte } from '@sveltejs/vite-plugin-svelte'
import { defineConfig } from 'vite'

export default defineConfig({
  plugins: [svelte()],
  optimizeDeps: { exclude: ['@kenn-io/kit-ui', 'moneyflow-views'] },
})
```

Import the styles once, after kit-ui's theme:

```ts
import '@kenn-io/kit-ui/theme.css'
import 'moneyflow-views/styles.css'
```

Every rule in `moneyflow-views/styles.css` applies only inside `.mf-views`, the root element of
each view component. It has no `html`, `body`, or `:root` rule. The views read kit-ui theme tokens,
including `--edge-highlight`, `--shadow-raised`, `--shadow-sunken`, `--shadow-panel`, and
`--bg-table-header`.

## Show a workspace

```svelte
<script lang="ts">
  import {
    MoneyflowWorkspace,
    historyViewState,
    viewsFor,
    type ProviderKind,
    type ViewID,
  } from 'moneyflow-views'

  interface Props {
    profileID: string
    provider: ProviderKind
    hostFetch: typeof fetch
  }

  let { profileID, provider, hostFetch }: Props = $props()
  let workspace = $state<ReturnType<typeof MoneyflowWorkspace>>()
  let shown = $state<ViewID>()
  const views = $derived(viewsFor(provider).filter((view) => view.group === 'explore'))
</script>

<nav aria-label="Moneyflow views">
  {#each views as view (view.id)}
    <button
      aria-current={view.id === shown ? 'page' : undefined}
      onclick={() => workspace?.open(view.id)}
    >
      {view.label}
    </button>
  {/each}
</nav>
<MoneyflowWorkspace
  bind:this={workspace}
  apiBase="/embedded/moneyflow/"
  fetch={hostFetch}
  {profileID}
  viewState={historyViewState(`/money/${profileID}/`)}
  onlocation={(view) => (shown = view)}
/>
```

| Prop | Default | Meaning |
| --- | --- | --- |
| `apiBase` | required | The `APIBasePath` the host mounts the API at |
| `fetch` | required | The host's `fetch`; it adds whatever the host's admission needs |
| `profileID` | required | The profile to show |
| `profileName` | none | Names the profile on the page shown when it cannot load, which otherwise says "this profile" |
| `viewState` | required | Where the view query and history state live |
| `readOnly` | `false` | `true` makes every edit, review, refresh, export, and commit control unavailable |
| `shortcuts` | `true` | `false` makes Moneyflow's keys act only while focus is inside its views, so they cannot collide with the host's shortcuts. Keys inside the views keep working, because the grid cannot be used without them |
| `autoRefresh` | `false` | `true` lets the page start a refresh that the server reports as due; leave it off when the host runs `RunScheduler` |
| `initialView` | none | The view to open when the history entry has no view of its own, such as the view a host's link names |
| `chrome` | `false` | `true` adds Moneyflow's own sidebar and page landmarks |
| `onlocation` | none | Reports the explore view shown after every change, or `undefined` when none is shown |
| `onreconnect` | none | The user asked to reconnect the provider |
| `onamazonimport` | none | The user asked to import Amazon orders; receives the saved `currency` and `scale` |

- The workspace reads `apiBase`, `fetch`, `viewState`, `autoRefresh`, and `initialView` when it
  mounts. A new `profileID` or `readOnly` starts it over, as if the host had just mounted it. To
  change another of those props, mount a new workspace.
- `initialView` opens once the first view has loaded, unless that view is already shown, so a
  link to the view that loads first sends no view change. A history entry with a view of its
  own, such as one that Back returns to, keeps it.
- Removing the workspace stops its work, and answers that arrive later change nothing.
- In a host, a refresh that the page starts, whether the user asks or `autoRefresh` starts it,
  opens a dialog that offers Close, not Cancel, because the refresh runs on after its request
  ends. Refreshes started elsewhere, such as by `RunScheduler`, an MCP tool, or another tab, show
  in the status strip.
- `viewsFor(kind)` lists the views of a provider kind in order: the explore views `merchant`,
  `category`, `group`, `account`, `time`, and `detail`, then the maintain views `duplicates`,
  `export`, `categories`, and `groups`, and `amazon` for Amazon profiles. In read-only mode, show
  only the explore views.
- `open(view)` shows a view as Moneyflow's own sidebar does. An explore view replaces the grid, a
  maintain view opens as a dialog, and `amazon` calls `onamazonimport`. A view asked for while the
  workspace loads opens once it has loaded. An unavailable view stays closed, and the status line
  says why.
- Call `open` when the user picks a view in the host's navigation. Opening an explore view applies
  it again even when it is shown, which leaves its drill-down and can clear its selection. Opening
  `detail` while it is shown at the top level changes nothing. Do not call `open` in answer to
  `onlocation`, and use `initialView`, not `open`, for the view a link names.
- `review()` opens what the Review button opens: the review of pending edits, or the status of a
  running provider write. With nothing to review, it shows the last write's status while the
  server still reports it, or else the status line says why. A review asked for while the
  workspace loads opens once it has loaded.
- Without `chrome`, the workspace adds no `main` landmark and no banner. It is a region named
  "Moneyflow workspace", and its command bar is a region named "View controls". Pending edits and
  provider status sit in a strip above the status line.
- Answer `onamazonimport` with `MoneyflowSetup`, `provider="amazon"`, the profile's ID, and the
  settings it received as `amazonSettings`.

### Keep the view in the host's history

`viewState` keeps the Moneyflow view query and a state object for each history entry:

```ts
interface ViewStateAdapter {
  readonly state: unknown // the state saved with the current entry, or null
  readonly query: string // the current entry's Moneyflow view query, without "?"
  push(state: unknown, query: string): void
  replace(state: unknown, query: string): void
  go(delta: number): void
  listen(restore: () => void): () => void // calls restore after back or forward
}
```

`historyViewState(path)` calls `history.pushState` and `history.replaceState` at `path` and owns
that page's query string. Use it only where nothing else writes browser history. With a client-side
router, implement `ViewStateAdapter` over the router's own history.

## Run setup

```svelte
<MoneyflowSetup
  apiBase="/embedded/moneyflow/"
  fetch={hostFetch}
  provider="monarch"
  name="Example Budget"
  oncomplete={(profileID) => showWorkspace(profileID)}
  oncancel={closeSetup}
/>
```

- Without `profileID`, setup creates a profile with the display name `name`, which it then
  requires. If the user cancels, setup removes that profile again. If the removal fails, setup
  says so and the profile stays in the list.
- A SimpleFIN profile stays once its setup starts, because setup claims the SimpleFIN connection
  for it.
- `local` completes as soon as the profile exists. `csv` completes at once for an existing profile
  and cannot create one.
- With `profileID`, setup reconnects or unlocks that profile.
- With `amazonSettings`, Amazon setup starts the import with those settings instead of asking.
- Setup reads its props once. Mount a new `MoneyflowSetup` for each setup. Removing it stops
  setup, and a profile it already created stays in `needs_setup`.
- Without `chrome`, setup adds no top bar, theme button, status bar, `main` landmark or banner,
  so it fits in the host's dialog or page. `chrome` frames it as a page of its own.

## Fetch contract

The views call the host's `fetch(input, init)`:

- `input` is a `Request`, a `URL`, or a URL string. Its path always starts with `<apiBase>api/v1/`.
- `POST` requests carry JSON with `Content-Type: application/json`, except Amazon file uploads,
  which send `FormData`.
- Every request sets `redirect: 'error'`. No request sets a token header or a `credentials`
  option, and none calls a bootstrap endpoint.
- Error responses are problem JSON. The views show its `detail`.

Standalone Moneyflow passes a `fetch` that adds its page and profile mutation tokens. A host passes
a `fetch` that adds whatever its own admission needs, such as a session cookie or a header.
