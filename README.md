# NekoLc Server

Golang implementation of the NekoLc API specification (v0.0.4). The server supports both file-based JSON configuration and database storage (MySQL/SQLite), with a visual admin dashboard for configuration management.

## Features

- **Database Storage**: Support for MySQL and SQLite to store all configurations
- **Visual Admin Dashboard**: Web-based UI at `/app/admin` for managing all settings
- **NekoLcApi Compliant**: Fully implements the NekoLc API specification
- **Flexible Configuration**: Fall back to JSON files when database config is not available

## Prerequisites

- Go 1.22+
- SQLite (built-in, no external dependencies) or MySQL 5.7+

## Database Configuration

The server supports three storage modes configured via `database.type` in `configs/app.json`:

### SQLite (Recommended for single-instance deployments)

```json
{
  "database": {
    "type": "sqlite",
    "sqlite": {
      "path": "./data/nekoserver.db"
    }
  }
}
```

### MySQL (Recommended for multi-instance deployments)

```json
{
  "database": {
    "type": "mysql",
    "mysql": {
      "host": "localhost",
      "port": 3306,
      "username": "app_user",
      "password": "secure_password",
      "database": "nekoserver",
      "params": "parseTime=true&charset=utf8mb4"
    }
  }
}
```

### In-Memory (For testing)

```json
{
  "database": {
    "type": "memory"
  }
}
```

## Configuration

Primary settings live in `configs/app.json`. The server will try to load configurations from the database first, falling back to JSON files if not found:

- `language.configPath` → localization bundle (`configs/languages.json`)
- `launcher.configPath` → launcher response template
- `maintenance.configPath` → maintenance windows per platform
- `news.configPath` → news/announcement items returned by `/v0/api/news`
- `update.configPath` → incremental/full update metadata

Set `APP_CONFIG_PATH` or pass `-config` to point at an alternate `app.json`.

## Run the server

```bash
# From the repository root
go run ./cmd/server
```

The server will:
1. Initialize the database (create tables if they don't exist)
2. Create a default admin user if no users exist (credentials printed to console)
3. Listen on the port defined in `server.port`

## Admin Dashboard

Access the visual admin dashboard at `http://localhost:8080/app/admin`. Features:

- **Launcher Configuration**: Manage hosts, WebSocket settings, security settings
- **Maintenance**: Enable/disable maintenance mode with custom messages
- **Updates**: Configure update packages for each platform and architecture. Upload files (hosted by the server with an auto-generated download URL), browse and scan server directories visually, search and sort items
- **News**: Create and manage multiple news items with search and sorting
- **Feedback**: View user feedback logs with search, sorting, deletion, and collapsible long entries
- **Users**: Manage user accounts, plus the **Account Policy** (`allowRegistration`, `requireEmail`, `verifyEmail`). When the account/authentication feature is disabled, the user-facing pages (`/app/login`, `/app/register`, `/app/dashboard`) show a "feature not enabled" notice.
- **Email & Home**: Configure SMTP (used for password recovery and email verification) and the Markdown home-page content shown on the user dashboard. Includes a "send test email" action.
- **Site Config**: Configure the site name, SEO description and a site announcement. The site name and SEO description are rendered server-side into the public home page (`<title>` / `<meta name="description">`), and a non-empty announcement is shown as a banner.

Uploaded update assets are served as static files at `/files/<relative-path>` (resolved within the configured update assets directory; path traversal is rejected).

### First Login

On first startup, the server creates a default admin account and prints the credentials:
```
IMPORTANT: Default admin account created!
Username: admin
Password: adm-xxxxxxxxxxxx
```

Use these credentials at `/app/login` to access the admin dashboard.

## Admin API Endpoints

The following admin API endpoints are available for configuration management (requires admin authentication):

- `GET /v0/api/admin/launcher` - Get launcher configuration
- `PUT /v0/api/admin/launcher` - Update launcher configuration
- `GET /v0/api/admin/maintenance` - Get maintenance configuration
- `PUT /v0/api/admin/maintenance` - Update maintenance configuration
- `GET /v0/api/admin/updates` - Get updates configuration
- `PUT /v0/api/admin/updates` - Update updates configuration
- `GET /v0/api/admin/news` - Get news items
- `PUT /v0/api/admin/news` - Update news items
- `POST /v0/api/admin/scanPath` - Scan a directory for update files
- `POST /v0/api/admin/generateUpdates` - Generate update config from directory
- `POST /v0/api/admin/uploadFile` - Upload a file (multipart `file`, optional `subdir`); stored under the update assets directory and served by this server. The response includes an absolute download `url` generated from the current request URL (scheme/host/base path)
- `GET /v0/api/admin/browseDir` - Browse sub-directories/files under the update assets directory (for the visual directory picker; optional `path` query)
- `DELETE /v0/api/admin/feedbackLogs/{id}` - Delete a single feedback log entry
- `GET /v0/api/admin/account` - Get account policy (`allowRegistration`, `requireEmail`, `verifyEmail`)
- `PUT /v0/api/admin/account` - Update account policy
- `GET /v0/api/admin/smtp` - Get SMTP settings (password redacted)
- `PUT /v0/api/admin/smtp` - Update SMTP settings (a blank/`********` password preserves the stored one)
- `POST /v0/api/admin/smtp/test` - Send a test email to a recipient
- `GET /v0/api/admin/homeContent` - Get the Markdown home-page content
- `PUT /v0/api/admin/homeContent` - Update the Markdown home-page content
- `GET /v0/api/admin/site` - Get site config (`siteName`, `seoDescription`, `announcement`)
- `PUT /v0/api/admin/site` - Update site config
- `GET /app/api/site-config` - Public site config (site name, SEO description, announcement)

## Account Operations

When authentication is enabled, the NekoLc web UI exposes account self-service endpoints under `/app/api`:

- `GET /app/api/me` - Authenticated account info (username, email, verification status, role)
- `GET /app/api/home-content` - Public home content (rendered Markdown HTML), current maintenance notice and recent news
- `POST /app/api/change-password` - Change password (requires current password)
- `POST /app/api/change-email` - Change email (requires current password; re-sends verification when `verifyEmail` is on)
- `POST /app/api/forgot-password` - Request a password reset email (always responds 200 to avoid leaking account existence)
- `POST /app/api/reset-password` - Reset a password with a single-use, time-limited token
- `POST /app/api/send-verification` - Send an email-verification link to the authenticated user
- `GET|POST /app/api/verify-email` - Verify an email address using a single-use token

Registration (`POST /app/register`) accepts an optional `email` in `registerRequest`. Registration is allowed only when `account.allowRegistration` is enabled (it defaults to `true` and may be turned off from the admin **Users → Account Policy** panel). When `account.requireEmail` is enabled, an email is mandatory; when `account.verifyEmail` is enabled, a verification email is sent on registration. Email addresses are unique per account when provided.

### WebSocket configuration

The server-side WebSocket listener is controlled by the `webSocket` section in `config.json`:

```json
"webSocket": {
  "enable": false,
  "port": "",
  "path": "/v0/ws"
}
```

- `enable` - turn the WebSocket hub on/off.
- `path` - the endpoint path (default `/v0/ws`).
- `port` - when empty (or equal to the main `server.port`), the endpoint is served on the main HTTP listener at `path`. When set to a different port, a dedicated listener is started on that local port serving only the WebSocket endpoint.

For backward compatibility the launcher config's `webSocket.enable` flag is still honored when the app-level flag is off.

### Email (SMTP) configuration

SMTP settings can be set in `config.json` (the `smtp` section) and overridden at runtime from the admin dashboard (stored in the database, hot-reloaded). When SMTP is disabled or unconfigured, the server runs normally and email-dependent flows degrade gracefully (tokens are still issued but no email is delivered). `tlsMode` accepts `starttls` (default), `tls` (implicit TLS) or `none`. `baseUrl` is the public base URL used to build links in emails.

The home-page content is authored in Markdown and rendered server-side with a dependency-free, XSS-safe renderer (all HTML is escaped; only a safe Markdown subset and a URL-scheme allowlist are honored).


## Authentication (JWT)

When authentication is enabled, the `/v0/api/auth/*` endpoints use JWTs signed with `authentication.jwtSecret` (HS256). Access tokens carry the `tokenType="access"` claim and expire according to `tokenExpirationSec`. Refresh tokens carry `tokenType="refresh"` and follow `refreshTokenExpirationDays`.

Authentication supports both username/password login and signature-based login for device authentication.

The `/v0/api/auth/login` endpoint is rate limited per client IP to mitigate brute-force attacks; once the limit is exceeded the server responds with HTTP 429 and a `TooManyRequests` error (NekoLc API spec v0.0.3+).

`GET /v0/api/auth/register` returns the registration page URL (`registerResponse.registerUrl`) with HTTP 200, or HTTP 501 when the account system is not available (NekoLc API spec v0.0.4).

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

```bash
go test ./...
```

## Request validation script

`scripts/test_requests.ps1` simulates common client flows (ping, launcher config, maintenance, updates, feedback, and an auth request). Run it after starting the server:

```powershell
pwsh -File scripts/test_requests.ps1 -BaseUrl "http://localhost:8080"
```

The script prints each request, the server response, and any errors encountered. Adjust the payloads inside the script to model additional client scenarios.
