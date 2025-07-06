# NekoLc Server

A GoLang implementation of the NekoLc API server based on the [api.md specification](./api.md).

## Features

This server implements all the APIs defined in the specification:

- **Testing endpoints** for connectivity and debugging
- **Authentication system** (optional) with JWT-like tokens
- **Launcher configuration** management
- **Maintenance status** checking
- **Update checking** system
- **Feedback logging** system
- Standard error handling with proper HTTP status codes
- Meta information in all API responses
- User preferences support

## Quick Start

### Build and Run

```bash
# Build the server
go build -o nekolc-server

# Run with default settings
./nekolc-server

# Run with authentication enabled
ENABLE_AUTH=true ./nekolc-server

# Run in debug mode (enables /v0/testing/echo endpoint)
DEBUG_MODE=true ./nekolc-server

# Run on custom port
PORT=9090 ./nekolc-server
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Server port |
| `API_VERSION` | `1.0.0` | API version |
| `MIN_API_VERSION` | `1.0.0` | Minimum API version |
| `BUILD_VERSION` | `20240601` | Build version |
| `RELEASE_DATE` | `2024-06-01T12:00:00Z` | Release date |
| `ENABLE_AUTH` | `false` | Enable authentication system |
| `DEBUG_MODE` | `false` | Enable debug endpoints |

## API Endpoints

### Testing

- `GET /v0/testing/ping` - Connectivity test
- `POST /v0/testing/echo` - Echo service (debug mode only)

### Authentication (Optional)

- `POST /v0/api/auth/login` - Login with credentials
- `POST /v0/api/auth/refresh` - Refresh access token
- `POST /v0/api/auth/validate` - Validate access token
- `POST /v0/api/auth/logout` - Logout and invalidate tokens

### Launcher

- `POST /v0/api/launcherConfig` - Get launcher configuration
- `POST /v0/api/maintenance` - Check maintenance status
- `POST /v0/api/checkUpdates` - Check for updates
- `POST /v0/api/feedbackLog` - Submit feedback logs

## Example Usage

### Test connectivity

```bash
curl -X GET "http://localhost:8080/v0/testing/ping"
```

Response:
```json
{
  "message": "pong",
  "status": "ok",
  "meta": {
    "apiVersion": "1.0.0",
    "minApiVersion": "1.0.0",
    "buildVersion": "20240601",
    "timestamp": 1751802495,
    "releaseDate": "2024-06-01T12:00:00Z",
    "deprecated": false,
    "deprecatedMessage": ""
  }
}
```

### Get launcher configuration

```bash
curl -X POST "http://localhost:8080/v0/api/launcherConfig" \
  -H "Content-Type: application/json" \
  -d '{
    "launcherConfigRequest": {
      "os": "windows",
      "arch": "x64"
    },
    "preferences": {
      "language": "en"
    }
  }'
```

### Login (when authentication is enabled)

```bash
curl -X POST "http://localhost:8080/v0/api/auth/login" \
  -H "Content-Type: application/json" \
  -d '{
    "auth": {
      "username": "admin",
      "password": "password"
    },
    "preferences": {
      "language": "en"
    }
  }'
```

## Testing

Run the test suite:

```bash
go test ./internal/handlers/... -v
```

## Architecture

The server is organized into the following packages:

- `internal/config` - Configuration management
- `internal/models` - Data models and types
- `internal/middleware` - HTTP middleware for common functionality
- `internal/handlers` - Request handlers for each endpoint
- `internal/api` - Router setup and API composition

## Standards Compliance

This implementation follows the NekoLc API specification:

- ✅ JSON request/response format
- ✅ Standard error response format
- ✅ Meta information in all responses
- ✅ Proper HTTP status codes (200, 204, 400, 401, 404, 500, 501, 503)
- ✅ Content-Type validation
- ✅ Authentication middleware (when enabled)
- ✅ User preferences support
- ✅ All required endpoints implemented

## Development

### Adding new endpoints

1. Define request/response models in `internal/models/`
2. Create handler in `internal/handlers/`
3. Add route in `internal/api/routes.go`
4. Add tests for the new functionality

### Extending authentication

The current authentication is simplified for demonstration. For production use:

1. Implement proper JWT token validation
2. Add user database integration
3. Implement token refresh logic
4. Add proper session management