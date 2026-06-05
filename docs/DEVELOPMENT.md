# Development Guide

This document covers building from source, project architecture, the full API reference, and contributing guidelines.

## Prerequisites

- Go 1.22 or later
- Git
- (Optional) MySQL 5.7+ if using MySQL storage

## Building from Source

```bash
# Clone the repository
git clone https://github.com/moehoshio/NekoLcServer.git
cd NekoLcServer

# Run directly (development)
go run ./cmd/server

# Build a binary
go build -o NekoLcServer ./cmd/server

# Cross-compile for Windows from Linux/macOS
GOOS=windows GOARCH=amd64 go build -o NekoLcServer.exe ./cmd/server
```

### Configuration for Development

The server loads `config.json` from the current working directory by default. Override with:

```bash
go run ./cmd/server -config /path/to/config.json
# or
APP_CONFIG_PATH=/path/to/config.json go run ./cmd/server
```

For development, `database.type: "memory"` is useful — it runs entirely in-memory with no external dependencies and no persistent data.

## Project Structure

```
NekoLcServer/
├── cmd/server/
│   └── main.go                 # Entry point, config loading, server startup
├── internal/
│   ├── server/
│   │   ├── server.go           # Server struct, router, middleware
│   │   ├── handlers.go         # NekoLc API handlers (launcher, updates, news, etc.)
│   │   ├── account.go          # Auth handlers (login, register, token refresh)
│   │   ├── admin_account.go    # Admin SMTP/account API handlers
│   │   ├── account_pages.go    # HTML page rendering
│   │   ├── websocket.go        # WebSocket hub and message handling
│   │   └── ratelimit.go        # Per-IP rate limiting
│   ├── store/
│   │   ├── store.go            # Store interface definition
│   │   ├── sqlite.go           # SQLite implementation
│   │   ├── mysql.go            # MySQL implementation
│   │   └── memory.go           # In-memory implementation
│   ├── config/
│   │   └── config.go           # Configuration structs and loading
│   ├── auth/
│   │   └── auth.go             # JWT token service (sign, verify, refresh)
│   ├── mailer/
│   │   └── mailer.go           # SMTP email sending
│   ├── markdown/               # XSS-safe Markdown renderer
│   └── localization/           # Multi-language support
├── scripts/
│   └── test_requests.ps1       # Client flow simulation script
├── .github/workflows/
│   ├── ci.yml                  # CI: build, vet, test
│   └── release.yml             # Release: cross-platform binary builds
├── config.json                 # Example configuration
├── go.mod / go.sum
└── README.md
```

## Architecture

### Request Flow

```
Client Request
  → chi Router (middleware: logging, CORS, auth)
    → Handler (internal/server/)
      → Store Interface (internal/store/)
        → SQLite / MySQL / Memory
```

### Key Design Decisions

- **Store Interface** — All data access goes through the `store.Store` interface, making it easy to swap backends or add new ones.
- **Config Fallback** — Configuration loads from the database first. If not found, it falls back to JSON files on disk. Admin dashboard writes always go to the database.
- **Hot-Reload** — File-based configs (`updates.json`, etc.) are watched for changes and reloaded automatically.
- **Graceful Shutdown** — The server handles SIGINT/SIGTERM and waits for in-flight requests to complete.

## Testing

```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run vet (static analysis)
go vet ./...
```

### Request Simulation Script

After starting the server, you can simulate client flows:

```powershell
pwsh -File scripts/test_requests.ps1 -BaseUrl "http://localhost:8080"
```

## API Reference

All API endpoints follow the [NekoLc API specification](https://github.com/moehoshio/NekoLcApi) (v0.0.4).

### Health Check

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/v0/testing/ping` | Returns `"pong"` — use for health checks |
| `POST` | `/v0/testing/echo` | Echoes the request body (debug mode only) |

### Launcher API (`/v0/api/`)

These are the endpoints that launcher clients call.

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| `POST` | `/v0/api/launcherConfig` | Optional | Get launcher configuration (hosts, features, security settings) |
| `POST` | `/v0/api/maintenance` | No | Check maintenance status for a given platform/architecture |
| `POST` | `/v0/api/checkUpdates` | No | Check for available updates for a given platform/architecture/version |
| `POST` | `/v0/api/news` | No | Get news items (supports pagination, filtering, sorting) |
| `POST` | `/v0/api/feedbackLog` | Optional | Submit a feedback/crash report |

### Authentication API (`/v0/api/auth/`)

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| `POST` | `/v0/api/auth/login` | No | Login with username/password or JWT signature |
| `POST` | `/v0/api/auth/refresh` | No | Get a new access token using a refresh token |
| `POST` | `/v0/api/auth/validate` | Bearer | Validate an access token |
| `POST` | `/v0/api/auth/logout` | Bearer | Revoke the current session (access + refresh tokens) |
| `GET` | `/v0/api/auth/register` | No | Check if registration is available (returns URL or 501) |
| `POST` | `/v0/api/auth/register` | No | Register a new account |

#### Login Request

```json
{
  "username": "user",
  "password": "pass"
}
```

#### Login Response

```json
{
  "accessToken": "eyJ...",
  "refreshToken": "eyJ...",
  "expiresIn": 3600,
  "tokenType": "Bearer"
}
```

#### Register Request

```json
{
  "username": "newuser",
  "password": "securepassword",
  "email": "user@example.com"
}
```

The `email` field is optional unless `account.requireEmail` is enabled.

### Admin API (`/v0/api/admin/`)

All admin endpoints require a valid JWT with admin role in the `Authorization: Bearer <token>` header.

#### Configuration Management

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/v0/api/admin/launcher` | Get launcher configuration |
| `PUT` | `/v0/api/admin/launcher` | Update launcher configuration |
| `GET` | `/v0/api/admin/maintenance` | Get maintenance configuration |
| `PUT` | `/v0/api/admin/maintenance` | Update maintenance configuration |
| `GET` | `/v0/api/admin/updates` | Get updates configuration |
| `PUT` | `/v0/api/admin/updates` | Update updates configuration |
| `GET` | `/v0/api/admin/news` | Get news items |
| `PUT` | `/v0/api/admin/news` | Update news items |
| `GET` | `/v0/api/admin/account` | Get account policy |
| `PUT` | `/v0/api/admin/account` | Update account policy |
| `GET` | `/v0/api/admin/smtp` | Get SMTP settings (password redacted) |
| `PUT` | `/v0/api/admin/smtp` | Update SMTP settings |
| `GET` | `/v0/api/admin/homeContent` | Get home page Markdown content |
| `PUT` | `/v0/api/admin/homeContent` | Update home page content |
| `GET` | `/v0/api/admin/site` | Get site config |
| `PUT` | `/v0/api/admin/site` | Update site config |

#### File & Update Operations

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/v0/api/admin/uploadFile` | Upload a file (multipart `file`, optional `subdir`) |
| `GET` | `/v0/api/admin/browseDir` | Browse files under the update assets directory |
| `POST` | `/v0/api/admin/scanPath` | Scan a directory for update files |
| `POST` | `/v0/api/admin/generateUpdates` | Auto-generate update config from scanned directory |
| `GET` | `/files/*` | Download uploaded/hosted files |

#### User Management

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/v0/api/admin/users` | List all users |
| `POST` | `/v0/api/admin/users` | Create a new user |
| `PUT` | `/v0/api/admin/users/{id}` | Update a user |
| `DELETE` | `/v0/api/admin/users/{id}` | Delete a user |

#### Feedback & Stats

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/v0/api/admin/feedbackLogs` | List feedback logs (with filtering/sorting) |
| `DELETE` | `/v0/api/admin/feedbackLogs/{id}` | Delete a feedback log entry |
| `GET` | `/v0/api/admin/feedbackFilterOptions` | Get available filter options for feedback |
| `GET` | `/v0/api/admin/stats` | Get API usage statistics |

#### Broadcasting

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/v0/api/admin/broadcast` | Send a notification to all connected WebSocket clients |
| `POST` | `/v0/api/admin/smtp/test` | Send a test email to verify SMTP configuration |

### Web UI API (`/app/api/`)

These endpoints serve the built-in web UI for user self-service.

| Method | Endpoint | Auth | Description |
|--------|----------|------|-------------|
| `GET` | `/app/api/me` | Bearer | Get current user info |
| `GET` | `/app/api/home-content` | No | Get rendered home page HTML, maintenance notice, recent news |
| `GET` | `/app/api/site-config` | No | Get public site config (name, description, announcement) |
| `POST` | `/app/api/login` | No | Login (sets session cookie) |
| `POST` | `/app/api/logout` | Bearer | Logout |
| `POST` | `/app/api/change-password` | Bearer | Change password (requires current password) |
| `POST` | `/app/api/change-email` | Bearer | Change email (requires current password) |
| `POST` | `/app/api/forgot-password` | No | Request password reset email |
| `POST` | `/app/api/reset-password` | No | Reset password with token |
| `POST` | `/app/api/send-verification` | Bearer | Send email verification link |
| `GET`/`POST` | `/app/api/verify-email` | No | Verify email with token |

### Web UI Pages (`/app/`)

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/app` | Home page |
| `GET` | `/app/login` | Login page |
| `GET` | `/app/register` | Registration page |
| `GET` | `/app/dashboard` | User dashboard |
| `GET` | `/app/admin` | Admin dashboard |
| `GET` | `/app/feedback` | Feedback submission form |
| `GET` | `/app/forgot-password` | Password recovery |
| `GET` | `/app/reset-password` | Password reset (with token) |
| `GET` | `/app/verify-email` | Email verification (with token) |

### WebSocket (`/v0/ws`)

Connect via WebSocket for real-time notifications. The endpoint path is configurable via `webSocket.path`.

## Database Schema

### Models

**User**
| Field | Type | Description |
|-------|------|-------------|
| `id` | int | Primary key |
| `username` | string | Unique username |
| `password` | string | bcrypt hash |
| `email` | string | Optional, unique when set |
| `emailVerified` | bool | Email verification status |
| `role` | string | `"user"` or `"admin"` |
| `createdAt` / `updatedAt` | time | Timestamps |

**RefreshToken**
| Field | Type | Description |
|-------|------|-------------|
| `id` | int | Primary key |
| `userId` | int | Owner |
| `tokenHash` | string | SHA-256 of the token |
| `expiresAt` | time | Expiration time |
| `revokedAt` | time | Null if active |

**AccountToken** (password reset / email verification)
| Field | Type | Description |
|-------|------|-------------|
| `id` | int | Primary key |
| `userId` | int | Owner |
| `tokenHash` | string | SHA-256 of the token |
| `purpose` | string | `"reset"` or `"verify"` |
| `expiresAt` | time | Expiration time |
| `usedAt` | time | Null if unused |

**FeedbackLog**
| Field | Type | Description |
|-------|------|-------------|
| `id` | int | Primary key |
| `userId` / `deviceId` | string | Sender identification |
| `platform` / `arch` | string | Client platform info |
| `content` | string | Feedback text |
| `clientInfo` | JSON | Additional client metadata |

**ConfigEntry** (database-stored configuration)
| Field | Type | Description |
|-------|------|-------------|
| `key` | string | Config key (e.g. `"launcher"`, `"news"`) |
| `value` | JSON | The configuration payload |

## Update Payload Format

Updates are organized by platform and architecture. Each architecture specifies `latest` (full packages) and optional `diffs` (incremental patches).

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
                "url": "https://example.com/updates/core-1.1.1.zip",
                "fileName": "core-1.1.1.zip",
                "downloadMeta": {
                  "hashAlgorithm": "sha256",
                  "suggestMultiThread": false
                }
              }
            ],
            "resource": [
              {
                "url": "https://example.com/updates/resource-1.1.0.zip"
              }
            ]
          },
          "diffs": [
            {
              "fromCoreVersion": "1.0.1",
              "core": [
                {
                  "url": "https://example.com/updates/1.0.1-to-1.1.1.zip",
                  "downloadMeta": { "hashAlgorithm": "sha256" }
                }
              ]
            }
          ]
        }
      }
    }
  }
}
```

### Directory-based Updates (`.path` support)

Instead of specifying individual URLs, you can point to a local directory:

```json
{
  "path": "/path/to/update-files",
  "baseUrl": "https://example.com/updates/",
  "downloadMeta": { "hashAlgorithm": "sha256" }
}
```

- All files in the directory are included with their relative paths
- Files are hashed automatically (SHA-256)
- `baseUrl` is prepended to form the download URL
- Directory scans are cached and refreshed when files change

## Adding a New Store Backend

1. Implement the `store.Store` interface in `internal/store/`
2. Add the new type to the switch in `cmd/server/main.go`
3. Add tests following the pattern in existing store tests

## CI/CD

- **CI** (`.github/workflows/ci.yml`): Runs `go vet`, `go test`, and `go build` on every push to `main` and on pull requests.
- **Release** (`.github/workflows/release.yml`): Triggered when a GitHub release is published. Builds Linux and Windows binaries and attaches them to the release.

## Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/my-change`
3. Make your changes and add tests
4. Run `go test ./...` and `go vet ./...`
5. Commit and push
6. Open a pull request
