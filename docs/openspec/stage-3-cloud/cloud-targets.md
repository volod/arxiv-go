# Cloud targets

Owner: `cloud-publishing`. Status: future; behavior below is the design baseline that the research
task must confirm or amend.

## Operator problem

After split, the video archive still has to be uploaded to the organization's cloud storage by hand,
and stub links point at a guessed URL. Publishing from `arxgo` keeps the directory tree, resumes
interrupted multi-gigabyte uploads, and writes the real links back.

## Operation

- `arxgo split ... --publish gdrive|sharepoint`: after each video commits (and previews finish),
  upload it; then rewrite the stub and registry `url`.
- `arxgo publish --archive PATH --video-archive PATH --publish TARGET`: publish an already split
  archive (new operation; the CLI reserves the name).
- `--publish-delete-local`: remove the local video archive copy after a verified upload (default
  off; restore then downloads, which is a further refinement and out of this stage's first task set).

## Target interface

```go
type Target interface {
    // EnsureFolder returns the remote id of rel_dir, creating missing parents.
    EnsureFolder(ctx context.Context, relDir string) (FolderRef, error)
    // Stat returns remote size/checksum for rel_path, or ErrNotFound.
    Stat(ctx context.Context, relPath string) (RemoteFile, error)
    // Upload streams the file with a resumable session; session state is persisted via the store.
    Upload(ctx context.Context, relPath string, src io.ReaderAt, size int64, sess SessionStore) (RemoteFile, error)
    // ShareLink returns the link written into stubs and registries.
    ShareLink(ctx context.Context, f RemoteFile) (string, error)
}
```

The WAL gains `publish_begin` (with session URL), `publish_chunk` (committed byte offset,
checkpoint-rate only) and `published` (remote id, link, checksum). A crash resumes the session from
the last server-acknowledged offset; an expired session restarts the upload.

## Google Drive

- API: Drive API v3 over `net/http`, resumable upload (`uploadType=resumable`), chunk size a
  multiple of 256 KiB (default 64 MiB), `Content-Range` resume after querying the session.
- Folder tree mirrored by `files.list` queries (`name`, `'parent' in parents`,
  `mimeType='application/vnd.google-apps.folder'`) with a per-run folder id cache; Shared Drives
  supported via `supportsAllDrives=true` and `--gdrive-drive-id`.
- Idempotency: `appProperties.arxgo_rel_path` and `md5Checksum` comparison (Drive reports MD5, so
  MD5 is computed during upload when publishing).
- Auth: service account JSON key (`--gdrive-credentials FILE`) or OAuth 2.0 device/installed-app
  flow with a token cache under the user config directory (`os.UserConfigDir()/arxgo/`), file mode
  0600. Library: `golang.org/x/oauth2` (pure Go). The full `google.golang.org/api` client is avoided
  unless research shows the REST surface is insufficient.
- Link: `webViewLink`; optional permission creation (`--share domain|anyone|none`, default none).
- Quotas: exponential backoff with jitter on 403 `rateLimitExceeded`/429/5xx.

## Microsoft SharePoint

- API: Microsoft Graph v1.0 `driveItem` endpoints for a site document library
  (`/sites/{site-id}/drives/{drive-id}`), `createUploadSession` with chunks that are multiples of
  320 KiB (default 60 MiB, below the 60 MiB per-request limit), resume via `nextExpectedRanges`.
- Folder tree via `PATCH /root:/{rel_dir}` with `folder` facet and
  `@microsoft.graph.conflictBehavior=fail`, treating 409 as exists.
- Idempotency: compare `size` and `file.hashes.quickXorHash` (computed locally during upload).
- Auth: Entra ID app registration; client credentials (certificate or secret from environment
  variable, never a flag value) or device code flow for delegated access. Token endpoint calls via
  `golang.org/x/oauth2`. `msgraph-sdk-go` is avoided for binary size unless research shows it is
  required.
- Link: `webUrl`; optional sharing link via `createLink` (`--share organization|anonymous|none`).
- Throttling: honor `Retry-After` on 429/503.

## Security

- Secrets come from files or environment variables and are redacted in logs (`slog` handler
  redaction for known keys and bearer tokens).
- Token caches and session state are 0600 on Linux and user-only ACL on Windows.
- No telemetry; only the configured endpoints are contacted.

## Acceptance

- Unit tests use `httptest.Server` fakes that replay recorded API exchanges (sanitized, committed
  as JSON), including chunk resume, expired session, 429 with `Retry-After`, and conflict on folder
  creation.
- Crash injection between chunks resumes without re-uploading acknowledged ranges.
- One live run per target against a human-provided test account/tenant, evidence kept outside the
  repository; this run is a declared `RUN NEEDED` step.
- Rerunning publish uploads nothing and rewrites no links.
