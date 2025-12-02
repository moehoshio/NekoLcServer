param(
    [string]$BaseUrl = "http://localhost:8080",
    [string]$JwtSecret = "your-secret-key-change-this-in-production",
    [string]$DeviceId = "device-uuid-script"
)

function Invoke-NekoRequest {
    param(
        [string]$Path,
        [string]$Method = "GET",
        $Body = $null
    )
    $uri = "$BaseUrl$Path"
    Write-Host "--- $Method $uri" -ForegroundColor Cyan
    try {
        if ($Body) {
            $json = $Body | ConvertTo-Json -Depth 6
            $response = Invoke-RestMethod -Method $Method -Uri $uri -Body $json -ContentType "application/json"
        }
        else {
            $response = Invoke-RestMethod -Method $Method -Uri $uri
        }
        $response | ConvertTo-Json -Depth 6
    }
    catch {
        Write-Warning $_
    }
    Write-Host ""
}

Invoke-NekoRequest -Path "/v0/testing/ping"

$unixTime = [int][Math]::Floor((Get-Date).ToUniversalTime().Subtract([datetime]'1970-01-01').TotalSeconds)
$loginTimestamp = $unixTime

function New-JwtSignature {
    param(
        [string]$Identifier,
        [long]$Timestamp,
        [string]$Secret
    )
    $payload = "${Identifier}:${Timestamp}:${Secret}"
    $sha = [System.Security.Cryptography.SHA256]::Create()
    try {
        $bytes = [System.Text.Encoding]::UTF8.GetBytes($payload)
        $hash = $sha.ComputeHash($bytes)
    }
    finally {
        $sha.Dispose()
    }
    [Convert]::ToBase64String($hash)
}

$clientInfo = @{ 
    clientInfo = @{ 
        app = @{ coreVersion = "1.0.0"; resourceVersion = "1.0.0" }
        system = @{ os = "windows"; arch = "x64" }
        deviceId = $DeviceId
    }
    timestamp = $unixTime
}

Invoke-NekoRequest -Path "/v0/api/launcherConfig" -Method "POST" -Body @{ launcherConfigRequest = $clientInfo; preferences = @{ language = "en" } }
Invoke-NekoRequest -Path "/v0/api/maintenance" -Method "POST" -Body @{ maintenanceRequest = $clientInfo }
Invoke-NekoRequest -Path "/v0/api/checkUpdates" -Method "POST" -Body @{ updateRequest = $clientInfo; preferences = @{ language = "en" } }
Invoke-NekoRequest -Path "/v0/api/feedbackLog" -Method "POST" -Body @{ feedbackLogRequest = @{ clientInfo = $clientInfo.clientInfo; timestamp = $clientInfo.timestamp; content = "Sample log" } }

$signature = New-JwtSignature -Identifier $DeviceId -Timestamp $loginTimestamp -Secret $JwtSecret
$loginBody = @{ 
    loginRequest = @{ 
        identifier = $DeviceId
        timestamp = $loginTimestamp
        signature = $signature
    }
}

Invoke-NekoRequest -Path "/v0/api/auth/login" -Method "POST" -Body $loginBody
