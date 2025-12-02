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

Entries in `diffFiles[].coreVersionPath` or `diffFiles[].resourceVersionPath` can point to JSON files that contain an array of download URLs (for example `"update/windows-64/1.0.1-to-1.1.1.json"`). Paths are resolved relative to the directory that holds `updates.json`, allowing one file per platform/version pair. When present, each URL in the array is returned to clients as an individual download item. If the path begins with `http://` or `https://`, it is treated as a direct file URL instead.

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
