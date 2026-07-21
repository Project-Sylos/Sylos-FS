# Cloud Provider Adapter Checklist

Use this checklist when adding a new OAuth cloud provider. Google Drive (`pkg/fs/googledrive/`) and Dropbox (`pkg/fs/dropbox/`) are reference implementations.

## Sylos-FS

1. **Provider constant** — Add to `pkg/cloud/providers.go`.
2. **Factory registration** — Implement `cloud.AdapterFactory` in `pkg/fs/{provider}/`:
   - `ProviderID()`
   - `NewSession(connectionID, StoredCredentials, TokenStore, FSDegradationState)`
   - `ListRoots(ctx, session)` for browse-before-migration
3. **Session** — Ref-counted via `ServiceManager`; holds refresh token metadata, `TokenStore` access token, degradation state.
4. **FSAdapter** — Implement `types.FSAdapter` + `CloudAdapterChecklist`:
   - `RegisterCredentials` / `Initialize` / `HasValidCredentials`
   - `ClassifyXxxError` + `DoWithClassifiedRetry` with **401 refresh as FS middleware**
     (`IsAuthFailure` + `Refresh`). Workers only wait on the adapter call; on refresh
     success the op retries; on refresh failure return a classified auth error.
     LocalFS omits Refresh (no tokens).
   - `FSDegradationReporter` on shared session state
   - `FSListChildrenPagination` with provider page limits
   - Optional batch mutations: `FSCreateFolderBatch` / `FSDeleteBatch` / `FSUploadFilesBatch` (Dropbox implements all three; ME uses `LeaseGroupBudget` when present). Graph `$batch` (max 20) is not used for these interfaces in v1.
   - Optional `FSTransferRestartPolicy` (+ `FSResumableWrite` when byte-resume in a fresh session is supported). Dropbox declares non-resumable overwrite-safe (upload sessions are not handed off); LocalFS seeks.
5. **Blank import** — Register factory in `pkg/fs/cloud_register.go`.
6. **Tests** — Classify errors, credential encrypt/decrypt round-trip (no live API required).

### Dropbox (implemented)

- Package: `pkg/fs/dropbox/` — `session.go`, `client.go`, `adapter.go`, `classify.go`, `transfer.go`, `batch.go`
- Roots: `user_root`, `team_space`, `team_folder`, `shared_folder` (see `pkg/cloud/roots.go`)
- Browse: `cloud.BrowseRoot(root)` passes namespace metadata via `driveId`
- Batch: `files/create_folder_batch` + `files/delete_batch` + `upload_session/start_batch` + `upload_session/finish_batch` (max 1000); ME pulls ~20k and leases ~1000 when these capabilities are present. File batch reduces commit RPCs (not bytes); appends stay per-chunk; finish_batch is serialized per adapter.
- OAuth token URI: `https://api.dropboxapi.com/oauth2/token`
- Scopes: `files.metadata.read`, `files.content.read`, `files.content.write`, `account_info.read`, `sharing.read`, `team_data.team_space`

### OneDrive / SharePoint (implemented)

- Shared Graph client: `pkg/fs/msgraph/`
- OneDrive: `pkg/fs/onedrive/` — My files + Shared (browse-only)
- SharePoint: `pkg/fs/sharepoint/` — Sites (browse-only) → document libraries (drives) + subsites; migration roots are drive roots or folders inside drives
- OAuth token URI: `https://login.microsoftonline.com/common/oauth2/v2.0/token`
- OneDrive scopes: `Files.ReadWrite.All`, `offline_access`, `User.Read`
- SharePoint scopes: `Sites.ReadWrite.All`, `Files.ReadWrite.All`, `offline_access`, `User.Read`

### Box (implemented)

- Package: `pkg/fs/box/` — `session.go`, `client.go`, `adapter.go`, `classify.go`, `transfer.go`
- Roots: **All Files** (`folder id 0`, `my_drive`)
- OAuth: authorize `https://account.box.com/api/oauth2/authorize`, token `https://api.box.com/oauth2/token`
- Scope: `root_readwrite` (Platform App + User Auth / OAuth 2.0)
- **Refresh tokens are single-use** — session rotates + persists via `CredentialsPersister` / `CloudConnectionOptions.PersistCredentials`
- Rate limits: ~1000 req/min/user general, ~240 upload/min/user; honor HTTP 429 + `Retry-After`
- Batch API max 20 (no uploads) — no `FS*Batch` in v1
- Upload: simple multipart ≤50MB; chunked upload sessions above that (spill buffer)

## Sylos-API

1. **Provider catalog** — Enable in `config.yaml` under `providers.{provider_id}`.
2. **Cloud service** — Map to `ServiceTypeCloud` via `services.LoadServices`.
3. **Routes** — Existing `/api/providers/...` handlers are provider-agnostic; token POST stores refresh only.
4. **SetRoot** — Requires `connectionId`; calls `initializePlanAdapters` after adapter acquire.
5. **Credential binding** — `creds/{connectionID}.enc` via `cloud.CredsRelPath`.

### Dropbox handoff (Sylos-API)

1. Enable `providers.dropbox` with `provider_id: dropbox`.
2. OAuth token POST: accept `StoredCredentials` JSON with `token_uri: "https://api.dropboxapi.com/oauth2/token"`.
3. Set `BackendGroupID = "conn:" + connectionID` on migration services.
4. Browse routes: `/api/providers/{id}/roots` — no new routes; verify `provider_id=dropbox` resolves via `cloud.Factory`.
5. When opening a migration root, pass `rootType` + `id` (+ `driveId` for team/shared folders) from `/roots` into `cloud.BrowseRoot`.

## Sylos-UI

### Dropbox handoff (Sylos-UI)

1. Add Dropbox to provider picker; use **Full Dropbox** app (not App folder).
2. OAuth authorization URL: `https://www.dropbox.com/oauth2/authorize`
   - Params: `client_id`, `response_type=code`, `token_access_type=offline`, `scope` (space-delimited), `redirect_uri`
3. Exchange code server-side; POST **refresh token only** to API.
4. Optionally prime access token via `Session.PrimeAccessToken`.
5. Roots picker: show **My Dropbox** always; Business accounts may also show **Team space**, **team folders**, and **shared folders** (multi-root UX like GDrive shared drives).

## Migration-Engine

1. **Scaling profile** — Add `{provider_id}` entry in `pkg/scaling/operation_profile.go`.
2. **Lifecycle** — `migration.Service.ProviderID` and `BackendGroupID = "conn:" + connectionID` set by Sylos-API.
3. **Error classification doc** — Note provider-specific throttle signals in `docs/fs_error_classification.md`.

## Token policy

- **Refresh token:** encrypted at rest in `creds/{connectionID}.enc`.
- **Access token:** in-memory only (`ConnectionManager` + `cloud.DefaultTokenStore`).
- **OAuth browser flow:** owned by Wails UI; API receives tokens via POST.
- **401 refresh:** FS adapter middleware (`DoWithClassifiedRetry`) refreshes and retries;
  opaque to Migration-Engine workers. Not the UI. Refresh failure → classified auth error.

## SFTP / FTP (non-OAuth)

See `pkg/fs/sftp/doc.go` and `pkg/fs/ftp/doc.go`. Form-based credentials, no OAuth tokens; same encrypted blob pattern for secrets.
