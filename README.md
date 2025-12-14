# NekoLc Server

Golang implementation of the NekoLc API specification. The server loads JSON configuration files from `configs/` and exposes the documented HTTP endpoints for launcher configuration, maintenance, updates, feedback logs, test utilities, and optional authentication.

## Prerequisites

- Go 1.22+
- Windows PowerShell 5.1 (for the included request script)

## Configuration

Primary settings live in `configs/app.json`. Additional referenced files are resolved relative to the working directory first and then to the directory containing `app.json`:

- `language.configPath` → localization bundle (`configs/languages.json`)
- `launcher.configPath` → launcher response template
- `maintenance.configPath` → maintenance windows per platform
- `update.configPath` → incremental/full update metadata
- `authentication.method` → `jwt` (default) or `account` (account mode currently returns 501)

Set `APP_CONFIG_PATH` or pass `-config` to point at an alternate `app.json`.

## Run the server

```powershell
# From the repository root
pwsh -File scripts/test_requests.ps1 -BaseUrl "http://localhost:8080" # optional validation script

# Start the server (ctrl+c to stop)
go run ./cmd/server
```

The server listens on the port defined in `server.port`. Optional authentication can be enabled via `authentication.enabled` and related fields.

### Authentication (JWT)

When authentication is enabled, the `/v0/api/auth/*` endpoints use JWTs signed with `authentication.jwtSecret` (HS256). Access tokens carry the `tokenType="access"` claim and expire according to `tokenExpirationSec`. Refresh tokens carry `tokenType="refresh"` and follow `refreshTokenExpirationDays`. Logout marks presented tokens as revoked until their natural expiration—set a strong, unique secret in production.

`authentication.method` controls the login flow:

- `jwt` (default): Login requests must provide `identifier`, `timestamp` (Unix seconds), and `signature = base64(SHA256(identifier:timestamp:jwtSecret))`. Requests using username/password are rejected with HTTP 400. The timestamp must be within ±10 minutes of server time unless `debug.enabled=true`.
- `account`: Not yet implemented; `/v0/api/auth/login` returns HTTP 501 when this method is selected.

### Update diff payloads

`updates.json` is organized by platform and architecture. Each architecture declares `latest` (core/resource versions plus download info) and optional `diffs` that transform a specific installed version to the latest. Each download is an array entry with `downloadMeta` and optional `fileName` override:

```json
{
  "platforms": {
    "windows": {
      "architectures": {
        "x64": {
          "latest": {
            "coreVersion": "1.1.1",
            "resourceVersion": "1.1.0",
            "core": [
              {
                "url": "https://example.com/updates/windows-x64-1.1.1.zip",
                "fileName": "windows-x64-1.1.1.zip",
                "downloadMeta": { "hashAlgorithm": "sha256", "suggestMultiThread": false }
              }
            ],
            "resource": [
              {
                "url": "https://example.com/updates/windows-x64-resource-1.1.0.zip",
                "fileName": "windows-x64-resource-1.1.0.zip",
                "downloadMeta": { "hashAlgorithm": "sha256", "suggestMultiThread": false }
              }
            ]
          },
          "diffs": [
            { "fromCoreVersion": "1.0.1", "core": [ { "url": "https://…/1.0.1-to-1.1.1.zip", "downloadMeta": { "hashAlgorithm": "sha256" } } ] },
            { "fromResourceVersion": "1.0.0", "resource": [ { "url": "https://…/1.0.0-to-1.1.0.zip", "downloadMeta": { "hashAlgorithm": "sha256" } } ] }
          ]
        }
      }
    }
  }
}
```

Diff entries may point to direct files (`url`) or to JSON lists of URLs via `path`; those paths resolve relative to the directory containing `updates.json` unless absolute or HTTP(S). Each returned file always includes `downloadMeta` with `hashAlgorithm`, `suggestMultiThread`, `isCoreFile`, and `isAbsoluteUrl` populated. Use `fileName` to control the saved filename; defaults to the basename of the URL.

### Update payloads (.path support)

`updates.json` entries can be a direct `url` or a local `path` that expands to many files.

- `path` points to a **directory**. All files under it are included using their relative paths (with forward slashes). Example: `path = /path/to/one` yields `img.png` and `logs/log.txt` if those files exist under that tree.
- `baseUrl` (optional) prefixes each emitted URL, e.g., `https://example.com/updates/windows-x64-0.0.1-files/` → `https://example.com/updates/windows-x64-0.0.1-files/img.png`.
- Hash and size: files from `path` are hashed with `downloadMeta.hashAlgorithm` (sha256 only) and returned as hex (no `sha256:` prefix) with `size` in bytes.
- Relative resolution: `path` is resolved relative to the directory containing `updates.json` unless absolute. `baseUrl` is not used for filesystem resolution—only URL output.
- Caching: directory scans are cached and refreshed automatically when file mtimes change; `updates.json` itself is hot-reloaded on change.
## Tests

Unit tests exercise core handlers via `httptest`:

```powershell
go test ./...
```

## Request validation script

`scripts/test_requests.ps1` simulates common client flows (ping, launcher config, maintenance, updates, feedback, and an auth request). Run it after starting the server:

```powershell
pwsh -File scripts/test_requests.ps1 -BaseUrl "http://localhost:8080"
```

The script prints each request, the server response, and any errors encountered. Adjust the payloads inside the script to model additional client scenarios.
