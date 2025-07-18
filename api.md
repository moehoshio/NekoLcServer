# NekoLc Api

This is the API specification document for [NekoLc](https://github.com/moehoshio/NekoLauncher), defining the API protocols, formats, error handling, and more for NekoLc.  
Servers can be implemented in any language, but all supported APIs should comply with these specifications.

Conventions:  
[Definition](#definition) [Protocol](#protocol) [Meta](#meta) [Preferences](#preferences)  

[Apis](#apis) :

- [Testing](#testing)
- [Api](#api)
  - [Account](#account)
    - [Launcher](#launcher)
    - [WebSocket](#websocket)
    - [Static Deployment](#static-deployment)

## Definition

1. "coreVersion": This represents the version of NekoLc.
2. "resourceVersion": This represents the version of any resources managed, maintained, or upgraded by NekoLc.

## Protocol

1. In most cases, we use JSON for data interaction.
2. The client and server must include the header: "Content-Type: application/json".
3. If authentication is required, include the header Authorization: Bearer {token} in the request.
4. Standard error response format:

    | Field | Type | Description | value/example |
    | --- | --- | --- | --- |
    | errors | array | List of error objects | ... |
    | error[].error | string | Error type | e.g ForClientError, ForServerError... |
    | error[].errorType | string | Error operation | e.g "InvalidRequest", "NotFound", "InternalError"... |
    | error[].errorMessage | string | A human-readable error message describing the issue | ... |
    | other fields | ... | Other content ... if any | ... |

    Example:

    ```json
    {
        "errors": [
            {
                "error": "ForClientError",
                "errorType": "InvalidRequest",
                "errorMessage": "The request is invalid."
            }
            // ...
        ]
        // other content ... if any
    }
    ```

5. HTTP status codes:  
    These represent the HTTP status codes that should be used in NekoLc, but not all APIs are required to use only these codes, nor are these the only possible codes that may be returned. For example, reverse proxy servers or CDNs may return other status codes. However, within NekoLc, these codes should be considered standard.

    - 200: Success, the request was processed successfully
    - 204: Request successful, no content to return
    - 206: Partial content returned successfully
    - 400: Client error, invalid request or format error, etc.
    - 401: Unauthorized, valid authentication credentials required
    - 404: Not found
    - 429: Too many requests, try again later
    - 500: Server error, internal error
    - 501: Method not supported, should be treated as a client error
    - 503: Service unavailable, the service is currently unavailable, such as during maintenance

## Meta

API meta information should be included in every API response, with the following structure:

| Field | Type | Description | value/example |
| --- | --- | --- | --- |
| meta | object | Meta information object | ... |
| meta.apiVersion | string | API version | "1.0.0" |
| meta.minApiVersion | string | Minimum required API version | "1.0.0" |
| meta.buildVersion | string | Build version | "20240601" |
| meta.timestamp | number | Server time (UTCZ Timestamp) | 1685625600 |
| meta.releaseDate | string | Release date (ISO 8601 format) | "YYYY-MM-DDTHH:MM:SSZ" |
| meta.deprecated | boolean | Whether this API version is deprecated | false |
| meta.deprecatedMessage | string | Deprecation info if deprecated | "This version is deprecated" |

Example:

```json
{
    "meta": {
        "apiVersion": "1.0.0",
        "minApiVersion": "1.0.0",
        "buildVersion": "20240601",
        "timestamp": 1685625600,
        "releaseDate": "2024-06-01T12:00:00Z",
        "deprecated": false,
        "deprecatedMessage": ""
    }
}
```

## preferences

The `preferences` field is used to pass user preferences, such as language settings. It should be included in the request body of APIs that support it.
Most APIs should support preferences. The structure is as follows:

| Field | Type | Description | value/example |
| --- | --- | --- | --- |
| preferences | object | User preferences object | ... |
| preferences.language | string | Preferred language | "en" |

Example:

```json
{
    "preferences": {
        "language": "en"
    }
}
```

For example, if supported, error messages can also be returned according to the preferred language.

## Apis

### /testing/

- `/v0/testing/ping` : get  
  - Tests connectivity, generally should not have restrictions
    - Custom return content, typically HTTP code 200 indicates success
- `/v0/testing/echo` : post , only debug
  - Post any content, and the server will return the same content.
    - Optional: Require verification of whether the authentication token header is correct and whether the format (such as JSON) is valid.
    - Note: This API should only be used in debug mode and must not be available in production environments.

### /api/

#### Account

- `/v0/api/auth/login` : post , optional

  - Obtain accessToken and refreshToken for authentication

    post：

    - Authentication with account and password (requires account system)

    | Field | Type | Description | value/example |
    | --- | --- | --- | --- |
    | auth.username | string | Username | "user" |
    | auth.password | string | Password | "pass" |
    | preferences | object | User preferences | ... |

    or

    - Authentication using a unique identifier to generate a hash value

    | Field | Type | Description | value/example |
    | --- | --- | --- | --- |
    | auth.identifier | string | Unique identifier | "device-uuid" |
    | auth.timestamp | number | UTCZ Timestamp | 1685625600 |
    | auth.signature | string | Hash signature | "abcdef..." |
    | preferences | object | User preferences | ... |

    Example:

    ```json
    {
        "auth": {
            "username": "user",
            "password": "pass"
        },
        "preferences": {
            // ...
        }
    }
    ```

    **response**：

    | Field | Type | Description | value/example |
    | --- | --- | --- | --- |
    | accessToken | string | Access token | "token-abc" |
    | refreshToken | string | Refresh token | "refresh-xyz" |
    | meta | object | Api meta information | ... |

    Example:

    ```json
    {
        "accessToken": "token-abc",
        "refreshToken": "refresh-xyz",
        "meta": {
            "apiVersion": "1.0.0"
        }
    }
    ```

    - If the account system is not implemented, return HTTP 501
    - If the account system is implemented but authentication fails, return HTTP 401

    - **About refreshToken validity:**
    - When a new refreshToken is obtained, it is recommended to immediately invalidate the previous refreshToken to enhance security and prevent reuse of old tokens.
    - If multi-device login is required, consider allowing multiple refreshTokens to exist in parallel, but each refreshToken should have its own validity period. This can be a fixed time (e.g., 30 days), or based on the last usage timestamp (e.g., expires 15 days after last use).

- `/v0/api/auth/refresh` : post , optional

  - Obtain a new accessToken using refreshToken

    post：

    | Field | Type | Description | value/example |
    | --- | --- | --- | --- |
    | refreshToken | string | Refresh token | "refresh-xyz" |

    Example:

    ```json
    {
        "refreshToken": "refresh-xyz"
    }
    ```

    **response**：

    | Field | Type | Description | value/example |
    | --- | --- | --- | --- |
    | accessToken | string | New access token | "token-abc" |
    | meta | object | Api meta information | ... |

    Example:

    ```json
    {
        "accessToken": "token-abc",
        "meta": {
            "apiVersion": "1.0.0"
        }
    }
    ```

  - If the account system is not implemented, return HTTP 501
  - If the refreshToken is invalid/expired, return HTTP 401

- `/v0/api/auth/validate` : post , optional

  - Validate the accessToken

    post：

    | Field | Type | Description | value/example |
    | --- | --- | --- | --- |
    | accessToken | string | Access token | "token-abc" |

    Example:

    ```json
    {
        "accessToken": "token-abc"
    }
    ```

    **response**：204 (No Content) for valid, 401 for invalid/expired

- `/v0/api/auth/logout` : post, optional

  - Immediately invalidate accessToken and refreshToken

    post：

    | Field | Type | Description | value/example |
    | --- | --- | --- | --- |
    | logout.accessToken | string | Access token | "token-abc" |
    | logout.refreshToken | string | Refresh token | "refresh-xyz" |

    Example:

    ```json
    {
        "logout": {
            "accessToken": "token-abc",
            "refreshToken": "refresh-xyz"
        }
    }
    ```

    **response**：204 (No Content) for success, 500 for server error

#### Launcher

- `/v0/api/launcherConfig` : post

  - Obtain the configuration of the launcher

    post：

    | Field | Type | Description | value/example |
    | --- | --- | --- | --- |
    | launcherConfigRequest | object | ... | ... |
    | launcherConfigRequest.os | string | OS | "windows" |
    | launcherConfigRequest.arch | string | Architecture | "x64" |
    | launcherConfigRequest.coreVersion | string | Core version (optional) | "1.0.0" |
    | launcherConfigRequest.resourceVersion | string | Resource version (optional) | "2.0.0" |
    | preferences | object | User preferences | ... |

    Example:

    ```json
    {
        "launcherConfigRequest": {
            "os": "windows",
            "arch": "x64"
        },
        "preferences": {
            "language": "zh-tw"
        }
    }
    ```

    **response**：

    | Field | Type | Description | value/example |
    | --- | --- | --- | --- |
    | launcherConfig | object | | ... |
    | launcherConfig.host | array | Host list | ["host1"] |
    | launcherConfig.webSocket | object | WebSocket config | ... |
    | launcherConfig.retryIntervalSec | number | Retry interval | 5 |
    | launcherConfig.maxRetryCount | number | Max retry count | 3 |
    | launcherConfig.security | object | Security config | ... |
    | launcherConfig.featuresFlags | object | Feature flags | ... |
    | meta | object | Api meta information | ... |

    **WebSocket**:

    | Field | Type | Description | value/example |
    | --- | --- | --- | --- |
    | webSocket | object | WebSocket config | ... |
    | webSocket.enable | boolean | Enable WebSocket | true |
    | webSocket.socketHost | string | WebSocket host | "wss://..." |
    | webSocket.heartbeatIntervalSec | number | Heartbeat interval | 30 |

    **Security**:

    | Field | Type | Description | value/example |
    | --- | --- | --- | --- |
    | security | object | Security config | ... |
    | security.enableAuthentication | boolean | Enable authentication | true |
    | security.tokenExpirationSec | number | Token expiration (seconds) | 3600 |
    | security.refreshTokenExpirationDays | number | Refresh token expiration (days) | 30 |
    | security.loginUrl | string | Login URL , can be empty to use default| "/login" |
    | security.logoutUrl | string | Logout URL , can be empty to use default| "/logout" |
    | security.refreshUrl | string | Refresh URL, can be empty to use default | "/refresh" |

    Example:

    ```json
    {
        "launcherConfig": {
            "host": ["host1"],
            "webSocket": {
                "enable": true,
                "socketHost": "wss://...",
                "heartbeatIntervalSec": 30
            },
            "retryIntervalSec": 5,
            "maxRetryCount": 3,
            "security": {
                "enableAuthentication": true,
                "tokenExpirationSec": 3600,
                "refreshTokenExpirationDays": 30,
                "loginUrl": "/login",
                "logoutUrl": "/logout",
                "refreshUrl": "/refresh"
            },
            "featuresFlags": {
                "ui": {
                    "enableDevHint": true
                },
                "enableFeatureA": true,
                "enableFeatureB": false
            }
        },
        "meta": {
            "apiVersion": "1.0.0"
        }
    }
    ```

  - The `featuresFlags` field can be extended as needed. These fields can be used to control the enabling or disabling of client features.

- `/v0/api/maintenance` : post

  - Check if the `service` is in maintenance mode

    post：

    | Field | Type | Description | value/example |
    | --- | --- | --- | --- |
    | checkMaintenance | object | Maintenance check parameters | ... |
    | checkMaintenance.os | string | OS | "windows" |
    | checkMaintenance.arch | string | Architecture | "x64" |
    | checkMaintenance.coreVersion | string | Core version (optional) | "1.0.0" |
    | checkMaintenance.resourceVersion | string | Resource version (optional) | "2.0.0" |
    | preferences | object | User preferences | ... |

    Example:

    ```json
    {
        "checkMaintenance": {
            "os": "windows",
            "arch": "x64"
        },
        "preferences": {
            "language": "zh-tw"
        }
    }
    ```

    **response**：

    | Field | Type | Description | value/example |
    | --- | --- | --- | --- |
    | maintenanceInformation.status | string | Maintenance status | "scheduled", "progress" |
    | maintenanceInformation.message | string | Maintenance message | "Planned maintenance" |
    | maintenanceInformation.startTime | string | Start time (ISO 8601 format) | "2024-06-01T12:00:00Z" |
    | maintenanceInformation.exEndTime | string | Expected end time (ISO 8601 format) | "2024-06-01T14:00:00Z" |
    | maintenanceInformation.posterUrl | string | Poster URL | "https://..." |
    | maintenanceInformation.link | string | Announcement link | "https://..." |
    | meta | object | Api meta information | ... |

    Example:

    ```json
    {
        "maintenanceInformation": {
            "status": "scheduled",
            "message": "Planned maintenance",
            "startTime": "2024-06-01T12:00:00Z",
            "exEndTime": "2024-06-01T14:00:00Z",
            "posterUrl": "https://...",
            "link": "https://..."
        },
        "meta": {
            "apiVersion": "1.0.0"
        }
    }
    ```

  - status: "scheduled", "progress"
    - "scheduled": Maintenance is planned but not yet started.
    - "progress": Maintenance is currently underway.
  - If the service is not in maintenance mode, return code 204 (No Content)

- `/v0/api/checkUpdates` : post

  - Check for updates

    post：

    | Field | Type | Description | value/example |
    | --- | --- | --- | --- |
    | checkUpdate | object | Update check parameters | ... |
    | checkUpdate.os | string | OS | "windows" |
    | checkUpdate.arch | string | Architecture | "x64" |
    | checkUpdate.coreVersion | string | Core version | "1.0.0" |
    | checkUpdate.resourceVersion | string | Resource version | "2.0.0" |
    | preferences | object | User preferences | ... |

    Example:

    ```json
    {
        "checkUpdate": {
            "os": "windows",
            "arch": "x64",
            "coreVersion": "1.0.0",
            "resourceVersion": "2.0.0"
        },
        "preferences": {
            "language": "zh-tw"
        }
    }
    ```

    ****response****：

  - If there are no updates, return code 204.
  - If the server is in maintenance mode, return code 503 (Service Unavailable) with maintenance information. (This applies to cases where the maintenance API and update API are not strongly consistent, and the update API is under maintenance. Specifically, if the maintenance status is checked before checking for updates, there is no need to handle separate maintenance status for the update API.)
  - If the request is invalid, return code 400 (Bad Request) with error information.
  - If there is an update, return code 200 and include update information.

    | Field | Type | Description | value/example |
    | --- | --- | --- | --- |
    | updateInformation | object | Update information | ... |
    | updateInformation.title | string | Update title | "New version" |
    | updateInformation.description | string | Update description | "Bug fixes" |
    | updateInformation.posterUrl | string | Poster URL | "https://..." |
    | updateInformation.publishTime | string | Publish time (ISO 8601 format) | "2024-06-01T12:00:00Z" |
    | updateInformation.resourceVersion | string | If this update does not involve a resource version, this key can be absent or an empty string | "2.0.1" |
    | updateInformation.isMandatory | boolean | Is mandatory update | true |
    | updateInformation.files | array | Update files | [...] |
    | meta | object | Api meta information | ... |

    **File Metadata**

    | Field | Type | Description | value/example |
    | --- | --- | --- | --- |
    | files | array | Update files | [...] |
    | files[].url | string | File download URL | "https://..." |
    | files[].fileName | string | File name | "main.exe" |
    | files[].checksum | string | File checksum | "abcdef..." |
    | files[].downloadMeta | object | Download metadata | ... |
    | files[].downloadMeta.hashAlgorithm | string | Hash algorithm | md5 , sha1 ,sha256 ,sha512 |
    | files[].downloadMeta.suggestMultiThread | boolean | Suggest multi-thread download | false |
    | files[].downloadMeta.isCoreFile | boolean | Is core file | true |
    | files[].downloadMeta.isAbsoluteUrl | boolean | If not absolute url , an use current host. | true |

    Example:

    ```json
    {
        "updateInformation": {
            "title": "New version",
            "description": "Bug fixes",
            "posterUrl": "https://...",
            "publishTime": "2024-06-01T12:00:00Z",
            "resourceVersion": "2.0.1",
            "isMandatory": true,
            "files": [
                {
                    "url": "https://...",
                    "fileName": "main.exe",
                    "checksum": "abcdef...",
                    "downloadMeta": {
                        "hashAlgorithm": "sha256",
                        "suggestMultiThread": false,
                        "isCoreFile": true,
                        "isAbsoluteUrl": true
                    }
                }
            ]
        },
        "meta": {
            "apiVersion": "1.0.0"
        }
    }
    ```

  - If the main program (i.e., Nekolc core, including libraries) needs to be updated, The main program, and main libraries should be included in the URL. The update program is then run, and main program exits.
  - The update program will update the main program and files by replacing them with the already downloaded versions, and then it will launch the main program.
  - If only resources need to be updated, the update is completed as soon as the download finishes.

- `/v0/api/feedbackLog` : post

  - Submit feedback logs

    post：

    | Field | Type | Description | value/example |
    | --- | --- | --- | --- |
    | feedbackLog | object | Feedback log information | ... |
    | feedbackLog.os | string | OS | "windows" |
    | feedbackLog.arch | string | Architecture | "x64" |
    | feedbackLog.coreVersion | string | Core version | "1.0.0" |
    | feedbackLog.resourceVersion | string | Resource version | "2.0.0" |
    | feedbackLog.timestamp | number | UTCZ Timestamp | 1685625600 |
    | feedbackLog.content | string | Feedback content | "Log content..." |
    | preferences | object | User preferences | ... |

    Example:

    ```json
    {
        "feedbackLog": {
            "os": "windows",
            "arch": "x64",
            "coreVersion": "1.0.0",
            "resourceVersion": "2.0.0",
            "timestamp": 1685625600,
            "content": "Log content..."
        },
        "preferences": {
            "language": "zh-tw"
        }
    }
    ```

  - Return 204 for success, 400 for client error, 500 for server error.
  - For example, if either the core or resource version is a non-existent version, return a client error.

### WebSocket

In the API, the use of WebSocket is optional.  
Whether it is enabled, the connection host, and other configurations are returned by the configuration API `/v0/api/launcherConfig`.  
The server can send update and maintenance notifications, and the client can report feedback information.  
This ensures real-time communication, preventing situations where a new version is released right after an update check.  

We only define the protocol and format for the WebSocket API, and the server can implement it as needed.  

Server-side WebSocket API should follow the following protocol:

| Field | Type | Description | value/example |
| --- | --- | --- | --- |
| action | string | Action type ("ping", "pong", "notify") | "notify" |
| messageId | string | Optional, message history compensation | "msg-123" |
| notifyChanged | object | Notification change object | ... |
| notifyChanged.type | string | Notification type ("update", "maintenance") | "update" |
| notifyChanged.os | string | OS | "windows" |
| notifyChanged.arch | string | Architecture | "x64" |
| notifyChanged.coreVersion | string | Core version | "1.0.0" |
| notifyChanged.resourceVersion | string | Resource version | "2.0.0" |
| notifyChanged.message | string | Notification message | "Update available" |
| errors | array | Standard error response format, if any | ... |
| meta | object | Api meta information | ... |

Example:

```json
{
    "action": "notify",
    "messageId": "msg-123",
    "notifyChanged": {
        "type": "update",
        "os": "windows",
        "arch": "x64",
        "coreVersion": "1.0.0",
        "resourceVersion": "2.0.0",
        "message": "Update available"
    },
    "meta": {
        "apiVersion": "1.0.0",
        "timestamp": "2024-06-01T12:00:00Z"
    }
}
```

Client-side WebSocket API should follow the following protocol:

| Field | Type | Description | value/example |
| --- | --- | --- | --- |
| action | string | Action type ("ping", "pong") | "ping" |
| accessToken | string | Optional, if authentication is enabled | "token-abc" |
| lastMessageId | string | Optional, message history compensation | "msg-122" |
| clientInfo | object | Client information | ... |
| clientInfo.os | string | OS | "windows" |
| clientInfo.arch | string | Architecture | "x64" |
| clientInfo.coreVersion | string | Core version | "1.0.0" |
| clientInfo.resourceVersion | string | Resource version | "2.0.0" |
| preferences | object | User preferences | ... |

Example:

```json
{
    "action": "ping",
    "accessToken": "token-abc",
    "lastMessageId": "msg-122",
    "clientInfo": {
        "os": "windows",
        "arch": "x64",
        "coreVersion": "1.0.0",
        "resourceVersion": "2.0.0"
    },
    "preferences": {
        "language": "zh-tw"
    }
}
```

Whether on the client or server side, if a ping request is received, a message with the action "pong" should be sent in response.

### Static Deployment

Some features support static deployment on the server side, but there are certain limitations:

- No authentication is possible.
- Incremental updates are not supported (each update requires a full download).
- No multilingual message support (including update notifications, maintenance information, etc.)
- No feedback mechanism.
- No differentiated adjustments; it is not possible to dynamically adjust based on client status, region, version, etc.
- For example, it is not possible to maintain only specific versions or specific clients.

For static deployment, we only define the API protocol format.

Remote configuration URL: GET  

**response**:

| Field | Type | Description | value/example |
| --- | --- | --- | --- |
| launcherConfig | object | Launcher configuration object | ... |
| launcherConfig.checkUpdateUrls | object | Update URLs by os-arch key | {"windows-x64": "..."} |
| maintenanceInformation | object | Maintenance information | ... |
| maintenanceInformation.status | string | Maintenance status ("scheduled", "progress", "completed") | "scheduled" |

Example:

```json
{
    "launcherConfig": {
        "checkUpdateUrls": {
            "windows-x64": "https://example.com/update/windows-x64.json"
        }
        // ...other launcherConfig fields...
    },
    "maintenanceInformation": {
        "status": "scheduled"
        // ...other maintenance fields...
    }
}
```

- If you only want to statically deploy the configuration to a CDN or hosting service, while leaving other logic to the backend, you can include only the `launcherConfig` field.

Check update URL: GET  

**response**:

| Field | Type | Description | value/example |
| --- | --- | --- | --- |
| coreVersion | string | Core version | "1.0.0" |
| resourceVersion | string | Resource version | "2.0.0" |
| updateInformation | object | Update information (same as update check) | ... |

Example:

```json
{
    "coreVersion": "1.0.0",
    "resourceVersion": "2.0.0",
    "updateInformation": {
        // ...same as update check format...
    }
}
```
