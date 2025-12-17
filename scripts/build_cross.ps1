param(
    [string]$OutputDir = "dist",
    [string[]]$Targets = @("windows/amd64", "linux/amd64", "linux/arm64", "darwin/arm64"),
    [string]$Main = "./cmd/server",
    [string]$BinaryName = "NekoLcServer",
    [switch]$Clean,
    [switch]$UseCGO
)

$ErrorActionPreference = "Stop"

# Remember original env so we can restore after building.
$originalGoos = $Env:GOOS
$originalGoarch = $Env:GOARCH
$originalCgo = $Env:CGO_ENABLED

try {
    if ($Clean -and (Test-Path $OutputDir)) {
        Write-Host "Cleaning output directory: $OutputDir"
        Remove-Item -Recurse -Force -Path $OutputDir
    }

    if (-not (Test-Path $OutputDir)) {
        New-Item -ItemType Directory -Path $OutputDir | Out-Null
    }

    foreach ($target in $Targets) {
        $parts = $target -split "[\\/]"
        if ($parts.Length -ne 2) {
            throw "Invalid target format '$target'. Use os/arch, e.g., windows/amd64"
        }
        $goos = $parts[0].ToLower()
        $goarch = $parts[1].ToLower()

        $Env:GOOS = $goos
        $Env:GOARCH = $goarch
        $Env:CGO_ENABLED = $(if ($UseCGO) { "1" } else { "0" })

        $ext = if ($goos -eq "windows") { ".exe" } else { "" }
        $outputName = "$BinaryName-$goos-$goarch$ext"
        $outputPath = Join-Path $OutputDir $outputName

        Write-Host "Building $target -> $outputPath (CGO_ENABLED=$Env:CGO_ENABLED)"
        go build -o $outputPath $Main
    }

    Write-Host "Build complete. Artifacts in '$OutputDir'."
}
finally {
    # Restore env
    $Env:GOOS = $originalGoos
    $Env:GOARCH = $originalGoarch
    $Env:CGO_ENABLED = $originalCgo
}
