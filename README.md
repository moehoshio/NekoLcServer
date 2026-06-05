<p align="center">
  <img src="docs/screenshots/banner.png" alt="NekoLc Server" width="600">
</p>

<h1 align="center">NekoLc Server</h1>

<p align="center">
  A modern launcher server for managing application updates, news, and user accounts — with a built-in admin dashboard.
</p>

<p align="center">
  <a href="https://github.com/moehoshio/NekoLcServer/releases">Download</a> &bull;
  <a href="#quick-start">Quick Start</a> &bull;
  <a href="#configuration">Configuration</a> &bull;
  <a href="#admin-dashboard">Admin Dashboard</a> &bull;
  <a href="#faq">FAQ</a> &bull;
  <a href="docs/DEVELOPMENT.md">Developer Docs</a>
</p>

---

## Features

- **Update Management** — Deliver full and incremental update packages to clients, with per-platform and per-architecture support
- **News & Announcements** — Publish news items and site-wide announcements visible to all launcher clients
- **User Accounts** — Optional registration, email verification, and password recovery
- **Admin Dashboard** — Web UI for managing everything without touching config files
- **Multi-Database** — SQLite (default), MySQL, or in-memory storage
- **Real-time Notifications** — WebSocket hub for pushing messages to connected clients
- **NekoLc API Compliant** — Fully implements the [NekoLc API specification](https://github.com/moehoshio/NekoLcApi) (v0.0.4)

## Quick Start

### 1. Download

Go to the [Releases page](https://github.com/moehoshio/NekoLcServer/releases) and download the binary for your platform:

| Platform | File |
|----------|------|
| Linux (x64) | `NekoLcServer-linux-amd64` |
| Windows (x64) | `NekoLcServer-windows-amd64.exe` |

### 2. Prepare Configuration

Create a `config.json` file in the same directory as the binary. Here is a minimal example:

```json
{
  "server": {
    "port": "8080"
  },
  "database": {
    "type": "sqlite",
    "sqlite": {
      "path": "./data/nekoserver.db"
    }
  }
}
```

> A full example `config.json` is included in the repository. You can download it from the [source code](https://github.com/moehoshio/NekoLcServer/blob/main/config.json) and modify it to suit your needs.

### 3. Run

**Linux:**

```bash
chmod +x NekoLcServer-linux-amd64
./NekoLcServer-linux-amd64
```

**Windows:**

Double-click `NekoLcServer-windows-amd64.exe`, or run from the command line:

```cmd
NekoLcServer-windows-amd64.exe
```

On first startup, the server will:

1. Create the database and tables automatically
2. Create a default **admin** account and print the credentials to the console:
   ```
   IMPORTANT: Default admin account created!
   Username: admin
   Password: adm-xxxxxxxxxxxx
   ```
3. Start listening on the configured port (default: `8080`)

> **Save the admin password!** It is only shown once. If you lose it, delete the database file and restart to generate a new one.

### 4. Open the Admin Dashboard

Visit `http://localhost:8080/app/admin` in your browser and log in with the admin credentials printed on first startup.

From the dashboard you can configure everything: updates, news, maintenance, users, email, and more.

<!-- Screenshot placeholder -->
<!-- ![Admin Dashboard](docs/screenshots/admin-dashboard.png) -->

---

## Configuration

All settings are stored in `config.json`. You can point to a different path using:

```bash
# Environment variable
export APP_CONFIG_PATH=/path/to/config.json
./NekoLcServer-linux-amd64

# Or command-line flag
./NekoLcServer-linux-amd64 -config /path/to/config.json
```

Below is a full reference of all configuration sections and their options.

### Server

```json
"server": {
  "appName": "Neko Launcher Server",
  "port": "8080",
  "basePath": ""
}
```

| Option | Description | Default |
|--------|-------------|---------|
| `appName` | Display name for the server | `"Neko Launcher Server"` |
| `port` | HTTP listen port | `"8080"` |
| `basePath` | URL prefix if running behind a reverse proxy (e.g. `"/launcher"`) | `""` |

### Database

```json
"database": {
  "type": "sqlite",
  "sqlite": {
    "path": "./data/nekoserver.db"
  },
  "mysql": {
    "host": "localhost",
    "port": 3306,
    "username": "app_user",
    "password": "secure_password",
    "database": "nekoserver",
    "params": "parseTime=true&charset=utf8mb4"
  }
}
```

| Option | Description | Default |
|--------|-------------|---------|
| `type` | Storage backend: `"sqlite"`, `"mysql"`, or `"memory"` | `"sqlite"` |
| `sqlite.path` | Path to SQLite database file | `"./data/nekoserver.db"` |
| `mysql.*` | MySQL connection parameters | — |

> **Tip:** Use `sqlite` for single-server setups. Use `mysql` if you need multiple server instances sharing the same database. Use `memory` only for testing — data is lost on restart.

### Authentication

```json
"authentication": {
  "enabled": true,
  "jwt": {
    "jwtSecret": "your-secret-key-change-this-in-production"
  },
  "tokenExpirationSec": 3600,
  "refreshTokenExpirationDays": 30
}
```

| Option | Description | Default |
|--------|-------------|---------|
| `enabled` | Enable/disable the account and auth system | `true` |
| `jwt.jwtSecret` | Secret key for signing JWT tokens. **Change this in production!** | — |
| `tokenExpirationSec` | Access token lifetime in seconds | `3600` (1 hour) |
| `refreshTokenExpirationDays` | Refresh token lifetime in days | `30` |

> **Security:** Always set a strong, unique `jwtSecret` in production. The default value is not secure.

### Account Policy

```json
"account": {
  "allowRegistration": true,
  "requireEmail": false,
  "verifyEmail": false
}
```

| Option | Description | Default |
|--------|-------------|---------|
| `allowRegistration` | Allow new users to register | `true` |
| `requireEmail` | Require an email address during registration | `false` |
| `verifyEmail` | Send a verification email on registration | `false` |

These settings can also be changed at runtime from the Admin Dashboard under **Users > Account Policy**.

### Email (SMTP)

- **Statistics**: Live server usage overview that auto-refreshes while open (no manual refresh needed); pick the time range.
- **Launcher Configuration**: Manage hosts, WebSocket settings, security settings
- **Maintenance**: Enable/disable maintenance mode with custom messages
- **Updates**: Configure update packages for each platform and architecture. Upload files (hosted by the server with an auto-generated download URL), browse and scan server directories visually, search and sort items
- **News**: Create and manage multiple news items with search and sorting
- **Feedback**: View user feedback logs with search, sorting, deletion, and collapsible long entries
- **Users**: Manage user accounts, plus the **Account Policy** (`allowRegistration`, `requireEmail`, `verifyEmail`). When the account/authentication feature is disabled, the user-facing pages (`/app/login`, `/app/register`, `/app/dashboard`) show a "feature not enabled" notice.
- **Email**: Configure SMTP (used for password recovery and email verification). Includes a "send test email" action.
- **Site Config**: Configure the site name, SEO description, a site announcement (banner), and the Markdown home-page content shown on the user dashboard. The site name and SEO description are rendered server-side into the public home page (`<title>` / `<meta name="description">`), and a non-empty announcement is shown as a banner.

The dashboard, user dashboard and public pages support **light / dark / auto** themes. The admin and user dashboards expose a theme switcher (Auto / Light / Dark, stored per-browser); other pages follow the operating-system `prefers-color-scheme` setting automatically. The interface is localized in English, Simplified Chinese and Traditional Chinese.

| Option | Description | Default |
|--------|-------------|---------|
| `enabled` | Enable SMTP email sending | `false` |
| `host` | SMTP server hostname | — |
| `port` | SMTP server port | `587` |
| `username` / `password` | SMTP credentials | — |
| `from` | Sender email address | — |
| `fromName` | Sender display name | `"NekoLc"` |
| `tlsMode` | TLS mode: `"starttls"`, `"tls"`, or `"none"` | `"starttls"` |
| `baseUrl` | Public URL of your server, used in email links | — |

> SMTP is required for password recovery and email verification. Without it, the server still works, but email features are disabled.

### Site

```json
"site": {
  "siteName": "NekoLcServer",
  "seoDescription": "A modern launcher server",
  "announcement": ""
}
```

| Option | Description | Default |
|--------|-------------|---------|
| `siteName` | Name shown in the browser tab and pages | `"NekoLcServer"` |
| `seoDescription` | Meta description for search engines | — |
| `announcement` | Banner text shown at the top of the user-facing pages. Leave empty to hide. | `""` |

### WebSocket

```json
"webSocket": {
  "enable": false,
  "port": "",
  "path": "/v0/ws"
}
```

| Option | Description | Default |
|--------|-------------|---------|
| `enable` | Enable the WebSocket hub | `false` |
| `port` | Dedicated port for WebSocket. Leave empty to share the main HTTP port. | `""` |
| `path` | WebSocket endpoint path | `"/v0/ws"` |

### Debug

```json
"debug": {
  "enabled": false
}
```

Enable debug mode to expose the `/v0/testing/echo` endpoint for troubleshooting.

---

## Admin Dashboard

The admin dashboard at `/app/admin` lets you manage the entire server visually.

### Overview

| Section | What You Can Do |
|---------|-----------------|
| **Launcher** | Configure hosts, retry behavior, security settings, and feature flags |
| **Maintenance** | Enable/disable maintenance mode with custom messages and per-platform overrides |
| **Updates** | Upload update packages, manage versions for each platform/architecture, browse server files |
| **News** | Create, edit, and manage news items with search and sorting |
| **Users** | Manage user accounts and registration policy (allow registration, require email, verify email) |
| **Feedback** | View, search, and delete user feedback logs |
| **Email** | Configure SMTP settings and send test emails |
| **Home Content** | Edit the Markdown content displayed on the user dashboard |
| **Site Config** | Set the site name, SEO description, and announcement banner |

<!-- Screenshot placeholders -->
<!-- ![Dashboard Overview](docs/screenshots/admin-overview.png) -->
<!-- ![Update Management](docs/screenshots/admin-updates.png) -->
<!-- ![News Management](docs/screenshots/admin-news.png) -->

### First Login

When you start the server for the first time, it prints admin credentials to the console. Use them to log in at `/app/login`, then navigate to `/app/admin`.

### Uploading Update Files

1. Go to **Updates** in the admin dashboard
2. Click **Upload File** and select your update package
3. The server stores the file and generates a download URL automatically
4. Configure the update entry to use the uploaded file

Uploaded files are served at `/files/<path>` with path-traversal protection.

---

## Running Behind a Reverse Proxy

If you run NekoLc Server behind Nginx, Caddy, or another reverse proxy:

1. Set `server.basePath` if the server is not at the root (e.g. `"/launcher"`)
2. Set `smtp.baseUrl` to your public URL so email links work correctly
3. Forward the `X-Forwarded-For` header for accurate rate limiting

**Nginx example:**

```nginx
location /launcher/ {
    proxy_pass http://127.0.0.1:8080/;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;

    # WebSocket support (if enabled)
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
}
```

---

## FAQ

### How do I reset the admin password?

Delete the database file (e.g. `./data/nekoserver.db`) and restart the server. A new admin account will be created and the credentials printed to the console.

> **Warning:** This deletes all data. If you use MySQL, drop the `users` table instead, or update the password hash directly in the database.

### The server won't start — "address already in use"

Another process is already using the configured port. Either:
- Stop the other process, or
- Change `server.port` in `config.json` to a different port

### Email verification / password reset emails are not being sent

1. Make sure `smtp.enabled` is `true`
2. Verify your SMTP credentials are correct
3. Use the **Send Test Email** button in the Admin Dashboard (Email section) to diagnose
4. Check your SMTP provider's logs for delivery issues
5. Ensure `smtp.baseUrl` is set to your server's public URL

### I see "feature not enabled" on the login page

Authentication is disabled. Set `authentication.enabled` to `true` in `config.json` and restart.

### How do I use MySQL instead of SQLite?

Change `database.type` to `"mysql"` and fill in the `database.mysql` section:

```json
{
  "database": {
    "type": "mysql",
    "mysql": {
      "host": "localhost",
      "port": 3306,
      "username": "your_user",
      "password": "your_password",
      "database": "nekoserver",
      "params": "parseTime=true&charset=utf8mb4"
    }
  }
}
```

Make sure the database exists before starting the server. Tables are created automatically.

### Can I run the server as a system service?

**Linux (systemd):**

Create `/etc/systemd/system/nekolcserver.service`:

```ini
[Unit]
Description=NekoLc Server
After=network.target

[Service]
Type=simple
WorkingDirectory=/opt/nekolcserver
ExecStart=/opt/nekolcserver/NekoLcServer-linux-amd64
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

Then:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now nekolcserver
```

### Where are logs stored?

Feedback logs from users are stored in the database. Server logs are printed to stdout/stderr — redirect them to a file if needed:

```bash
./NekoLcServer-linux-amd64 2>&1 | tee server.log
```

### How do I back up my data?

- **SQLite:** Copy the database file (default: `./data/nekoserver.db`)
- **MySQL:** Use `mysqldump` as usual
- **Config:** Back up your `config.json`

---

## Screenshots

> Screenshots will be added here to illustrate the admin dashboard and user-facing pages.

<!-- 
Add screenshots to the docs/screenshots/ directory and uncomment the lines below:

![Home Page](docs/screenshots/home.png)
*The user-facing home page with news and announcements*

![Admin Dashboard](docs/screenshots/admin-dashboard.png)
*The admin dashboard overview*

![Update Management](docs/screenshots/admin-updates.png)
*Managing update packages per platform*

![News Editor](docs/screenshots/admin-news.png)
*Creating and managing news items*

![User Management](docs/screenshots/admin-users.png)
*Managing user accounts and registration policy*

![SMTP Configuration](docs/screenshots/admin-smtp.png)
*Configuring email settings with test send*
-->

---

## Contributing

See [Developer Documentation](docs/DEVELOPMENT.md) for build instructions, project architecture, API reference, and development setup.

## License

See [LICENSE](LICENSE) for details.
