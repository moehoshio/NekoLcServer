# NekoLc Server

A production-ready GoLang implementation of the NekoLc API server based on the [api.md specification](./api.md).

## 🚀 Features

This server implements all the APIs defined in the specification with enterprise-grade functionality:

### Core Features
- **Testing endpoints** for connectivity and debugging
- **JWT Authentication system** with ID + timestamp signature support
- **Configuration-driven architecture** with external JSON config files
- **Multiple storage backends** (SQLite, file-based, MySQL support)
- **Command line interface** with flag-based configuration
- **Hot-reload configuration** support
- **Launcher configuration** management
- **Maintenance status** checking with OS/arch-specific settings
- **Advanced update checking** with incremental and full package support
- **Feedback logging** with persistent storage
- **Multi-language support** (English, Traditional Chinese, extensible)

### Technical Features
- ✅ **Pure Go SQLite** implementation (no CGO dependencies)
- ✅ **Flexible storage backends** (SQLite, file-based, MySQL)
- ✅ **Command line flag configuration** with proper precedence
- ✅ **Hot-reload functionality** for configuration updates
- ✅ **Platform-specific maintenance** and update management
- ✅ **Incremental update support** with fallback to full packages
- ✅ **JWT token generation and validation**
- ✅ **Token revocation and refresh mechanism**
- ✅ **Configuration files instead of hardcoded values**
- ✅ **Localized error messages and UI text**
- ✅ **Standard error handling with proper HTTP status codes**
- ✅ **Meta information in all API responses**
- ✅ **Production-ready deployment configuration**

## 📁 Project Structure

```
├── configs/                    # Configuration files
│   ├── app.json               # Main application config
│   ├── launcher.json          # Launcher-specific settings
│   ├── maintenance.json       # Maintenance status config (with platform support)
│   ├── updates.json           # Update configuration (new)
│   ├── languages.json         # Localization strings
│   └── production.example.json # Production config template
├── data/                      # Database and data files (created at runtime)
├── internal/
│   ├── auth/                  # JWT authentication logic
│   ├── config/                # Configuration loading with CLI support
│   ├── handlers/              # API endpoint handlers
│   ├── middleware/            # HTTP middleware
│   ├── models/                # Request/response models
│   └── storage/               # Storage abstraction (SQLite, file, MySQL)
│       ├── database.go        # SQLite implementation
│       ├── file.go           # File-based storage
│       └── factory.go        # Storage factory
└── main.go                    # Application entry point with CLI support
```

## 🖥️ Command Line Interface

The server supports comprehensive command-line configuration with proper precedence: 
**CLI flags > Config files > Defaults**

### Usage
```bash
./nekolc-server [options]
```

### Available Options
```bash
--config_path=PATH     Path to configuration files directory (default: ./configs)
--port=PORT           Server port (default: 8080)
--debug=BOOL          Enable debug mode (default: false)
--enable_auth=BOOL    Enable authentication (default: false)
--jwt_secret=SECRET   JWT secret key
--database_type=TYPE  Database type: sqlite, mysql, file (default: sqlite)
--database_path=PATH  Database connection path
--reload              Hot-reload configuration and exit
--help                Show help message
```

### Examples
```bash
# Start with custom config path
./nekolc-server --config_path=/etc/nekolc --port=9000

# Enable authentication and debug mode
./nekolc-server --enable_auth=true --debug=true

# Use file-based storage
./nekolc-server --database_type=file --database_path=/var/lib/nekolc/storage

# Hot-reload configuration
./nekolc-server --reload
```

### Storage Backend Configuration

#### SQLite (Default)
```bash
--database_type=sqlite --database_path=./data/server.db
```

#### File-based Storage
```bash
--database_type=file --database_path=/var/lib/nekolc/data
```

#### MySQL (Future)
```bash
--database_type=mysql --database_path="user:password@tcp(localhost:3306)/dbname"
```

## 🔧 Configuration

The server uses JSON configuration files in the `configs/` directory:

### Main Configuration (`configs/app.json`)
```json
{
  "server": {
    "port": "8080",
    "apiVersion": "1.0.0",
    "buildVersion": "20241201"
  },
  "authentication": {
    "enabled": true,
    "jwtSecret": "your-secure-secret-key",
    "tokenExpirationSec": 3600
  },
  "database": {
    "type": "sqlite",
    "path": "./data/nekolc.db"
  },
  "storage": {
    "basePath": "./data"
  }
}
```

### Update Configuration (`configs/updates.json`)
```json
{
  "latestCoreVersion": "1.1.1",
  "latestResourceVersion": "1.1.0",
  "files": [
    {
      "os": "windows",
      "arch": "x64",
      "coreVersion": "1.0.1",
      "coreVersionPath": "update/windows-64/1.0.1-to-1.1.1.json"
    }
  ],
  "fullPackages": {
    "windows-x64": {
      "coreVersion": "1.1.1",
      "resourceVersion": "1.1.0",
      "downloadUrl": "https://example.com/updates/windows-x64-1.1.1.zip",
      "size": 1024000,
      "checksum": "sha256:abc123..."
    }
  }
}
```

### Maintenance Configuration (`configs/maintenance.json`)
```json
{
  "maintenanceActive": false,
  "maintenanceInfo": {
    "status": "scheduled",
    "message": "Scheduled maintenance"
  },
  "platformSpecific": {
    "windows-x64": {
      "maintenanceActive": true,
      "maintenanceInfo": {
        "status": "progress",
        "message": "Windows servers under maintenance"
      }
    }
  }
}
```

### Environment Variable Overrides (Legacy)
```bash
export PORT=8080
export ENABLE_AUTH=true
export DEBUG_MODE=false
export JWT_SECRET="your-secure-secret-key"
export CONFIG_PATH="./configs"
```

## 🚀 Quick Start

### Build and Run

```bash
# Install dependencies
go mod tidy

# Build the server
go build -o nekolc-server ./main.go

# Run with default settings (auth disabled, debug off)
./nekolc-server

# Run with authentication enabled
ENABLE_AUTH=true JWT_SECRET="secure-key" ./nekolc-server

# Run with custom config path
# Run with custom config path
CONFIG_PATH="/etc/nekolc" ./nekolc-server
```

## 🔐 Authentication

The server supports two authentication methods:

### 1. Username/Password Authentication
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

### 2. ID + Timestamp Signature Authentication
```bash
# Generate signature: SHA256(identifier + timestamp + secret)
identifier="device-12345"
timestamp=$(date +%s)
secret="your-jwt-secret"
signature=$(echo -n "${identifier}${timestamp}${secret}" | sha256sum | cut -d' ' -f1)

curl -X POST "http://localhost:8080/v0/api/auth/login" \
  -H "Content-Type: application/json" \
  -d "{
    \"auth\": {
      \"identifier\": \"$identifier\",
      \"timestamp\": $timestamp,
      \"signature\": \"$signature\"
    },
    \"preferences\": {
      \"language\": \"en\"
    }
  }"
```

### Token Management
- **Access tokens** expire in 1 hour (configurable)
- **Refresh tokens** expire in 30 days (configurable)
- All tokens are stored in SQLite database for revocation tracking
- Tokens can be revoked via logout endpoint

## 🌐 Multi-language Support

The server supports localized error messages and UI text:

```json
// configs/languages.json
{
  "en": {
    "errors": {
      "InvalidRequest": "The request is invalid.",
      "Unauthorized": "Authentication required."
    }
  },
  "zh-tw": {
    "errors": {
      "InvalidRequest": "請求無效。",
      "Unauthorized": "需要身份驗證。"
    }
  }
}
```

## 📊 Database Storage

### Feedback Logs
All feedback logs are stored in SQLite with full metadata:
```sql
CREATE TABLE feedback_logs (
    id INTEGER PRIMARY KEY,
    os TEXT NOT NULL,
    arch TEXT NOT NULL,
    core_version TEXT NOT NULL,
    resource_version TEXT NOT NULL,
    timestamp INTEGER NOT NULL,
    content TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### Authentication Tokens
JWT tokens are tracked for revocation:
```sql
CREATE TABLE auth_tokens (
    id INTEGER PRIMARY KEY,
    token_hash TEXT UNIQUE NOT NULL,
    token_type TEXT NOT NULL,
    user_id TEXT NOT NULL,
    expires_at DATETIME NOT NULL,
    is_revoked BOOLEAN DEFAULT FALSE
);
```

## 🧪 Testing

### Unit Tests
```bash
# Run all tests
go test ./... -v

# Run specific component tests
go test ./internal/auth/... -v
go test ./internal/handlers/... -v
go test ./internal/storage/... -v
```

### API Testing Examples

#### Test connectivity:
```bash
curl -X GET "http://localhost:8080/v0/testing/ping"
```

#### Get launcher configuration:
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

#### Submit feedback log:
```bash
curl -X POST "http://localhost:8080/v0/api/feedbackLog" \
  -H "Content-Type: application/json" \
  -d '{
    "feedbackLog": {
      "os": "windows",
      "arch": "x64",
      "coreVersion": "1.0.0",
      "resourceVersion": "2.0.0",
      "timestamp": 1685625600,
      "content": "Test feedback log"
    },
    "preferences": {
      "language": "en"
    }
  }'
```

## 🚀 Production Deployment

### Configuration Security
1. Copy `configs/production.example.json` to `configs/app.json`
2. Update `jwtSecret` with a secure random key
3. Set appropriate file permissions:
```bash
chmod 600 configs/app.json  # Protect sensitive config
chmod 700 data/             # Protect database directory
```

### Environment Variables
```bash
export ENABLE_AUTH=true
export JWT_SECRET="$(openssl rand -base64 32)"
export PORT=8080
export CONFIG_PATH="/etc/nekolc/configs"
```

### Systemd Service Example
```ini
[Unit]
Description=NekoLc API Server
After=network.target

[Service]
Type=simple
User=nekolc
Group=nekolc
WorkingDirectory=/opt/nekolc
ExecStart=/opt/nekolc/nekolc-server
Environment=ENABLE_AUTH=true
Environment=CONFIG_PATH=/etc/nekolc/configs
Environment=JWT_SECRET=your-production-secret
Restart=always

[Install]
WantedBy=multi-user.target
```

## 📋 Environment Variables Reference

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | Server port |
| `ENABLE_AUTH` | `false` | Enable JWT authentication |
| `DEBUG_MODE` | `false` | Enable debug endpoints |
| `JWT_SECRET` | `default-secret-change-this` | JWT signing secret |
| `CONFIG_PATH` | `./configs` | Configuration files directory |
| `API_VERSION` | From config | Override API version |
| `BUILD_VERSION` | From config | Override build version |

## 🔧 Configuration Files Reference

### Required Files
- `configs/app.json` - Main application configuration
- `configs/launcher.json` - Launcher-specific settings
- `configs/maintenance.json` - Maintenance status configuration
- `configs/languages.json` - Localization strings

### Optional Files
- `configs/production.json` - Production overrides
- `configs/secrets.json` - Sensitive configuration (add to .gitignore)

## 🔍 API Compliance

This implementation fully complies with the [api.md specification](./api.md):

- ✅ All endpoints implemented
- ✅ Standard error response format
- ✅ Meta information in responses
- ✅ Proper HTTP status codes
- ✅ JSON request/response format
- ✅ User preferences support
- ✅ Authentication flow (optional)
- ✅ Maintenance and update checking
- ✅ Feedback logging system

## 🤝 Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/amazing-feature`
3. Commit your changes: `git commit -m 'Add amazing feature'`
4. Push to the branch: `git push origin feature/amazing-feature`
5. Open a Pull Request

## 📝 License

This project is licensed under the MIT License - see the LICENSE file for details.

## 🙏 Acknowledgments

- Built according to the NekoLc API specification
- Uses industry-standard JWT for authentication
- Implements proper security practices for production use
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