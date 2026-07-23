# Vorn Media Server bare-metal installer for Windows.
#
# NOTE: written to Windows service conventions but not tested on real
# Windows (this was built in a Linux-only environment) -- review before
# relying on it, and please report any issues.
#
# What this does:
#   - Downloads (or copies, via -BinaryPath) vornd.exe to
#     C:\Program Files\Vorn\vornd.exe
#   - Writes C:\ProgramData\Vorn\vornd.env (edit this before starting the
#     service; only created if it doesn't already exist)
#   - Registers vornd as a Windows service (via sc.exe) that loads that env
#     file itself at startup -- no NSSM or other third-party service wrapper
#     needed, since Vorn is a plain foreground process that handles
#     SIGTERM-equivalent shutdown cleanly
#   - Starts the service
#
# What this does NOT do -- you need these already, locally or reachable
# over the network:
#   - PostgreSQL (https://www.postgresql.org/download/windows/)
#   - A Redis-protocol server (DragonflyDB or Redis; both need WSL2 or a
#     remote host on native Windows, since neither ships a native Windows
#     build)
#   - ffmpeg (https://www.gyan.dev/ffmpeg/builds/; needed for transcoding
#     only, direct-play still works without it)
#
# Usage (run as Administrator):
#   .\install.ps1 -BinaryPath C:\path\to\vornd.exe
#   .\install.ps1 -Version v1.2.3   # downloads that release from GitHub
#   .\install.ps1                   # downloads the latest release

param(
    [string]$BinaryPath = "",
    [string]$Version = "latest"
)

$ErrorActionPreference = "Stop"

$InstallDir = "$env:ProgramFiles\Vorn"
$ConfigDir = "$env:ProgramData\Vorn"
$Repo = "eoghan2t9/Vorn-Media-Server"
$ServiceName = "vornd"

$currentPrincipal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $currentPrincipal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    Write-Error "This installer needs an elevated (Administrator) PowerShell session."
    exit 1
}

Write-Host "==> Checking prerequisites (not installed automatically -- see the project README)"
foreach ($cmd in @("ffmpeg", "ffprobe")) {
    if (-not (Get-Command $cmd -ErrorAction SilentlyContinue)) {
        Write-Host "  warning: $cmd not found on PATH. Transcoding will be unavailable until it's installed; direct-play still works."
    }
}
Write-Host "  Make sure PostgreSQL and a Redis-protocol server (DragonflyDB or Redis) are reachable --"
Write-Host "  this installer doesn't provision them."
Write-Host ""

if (-not $BinaryPath) {
    $arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else {
        Write-Error "Unsupported architecture: only 64-bit Windows is supported."
        exit 1
    }

    Write-Host "==> Downloading vornd ($Version, windows/$arch) from GitHub Releases"
    $tmpDir = New-Item -ItemType Directory -Path (Join-Path $env:TEMP "vorn-install-$(Get-Random)")
    try {
        $url = if ($Version -eq "latest") {
            "https://github.com/$Repo/releases/latest/download/vornd_windows_${arch}.exe.zip"
        } else {
            "https://github.com/$Repo/releases/download/$Version/vornd_windows_${arch}.exe.zip"
        }

        $zipPath = Join-Path $tmpDir "vornd.zip"
        Invoke-WebRequest -Uri $url -OutFile $zipPath
        Expand-Archive -Path $zipPath -DestinationPath $tmpDir
        $BinaryPath = Join-Path $tmpDir "vornd.exe"
    } catch {
        Remove-Item -Recurse -Force $tmpDir -ErrorAction SilentlyContinue
        throw
    }
}

if (-not (Test-Path $BinaryPath)) {
    Write-Error "Binary not found at: $BinaryPath"
    exit 1
}

Write-Host "==> Stopping existing service (if any)"
if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
    Stop-Service -Name $ServiceName -ErrorAction SilentlyContinue
}

Write-Host "==> Installing binary to $InstallDir\vornd.exe"
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Copy-Item -Path $BinaryPath -Destination "$InstallDir\vornd.exe" -Force

New-Item -ItemType Directory -Force -Path $ConfigDir | Out-Null
$envFile = "$ConfigDir\vornd.env"
if (-not (Test-Path $envFile)) {
    Write-Host "==> Writing default config to $envFile (edit this before starting the service)"
    @"
# Vorn Media Server configuration. See the project README for the full list
# of environment variables (torrent/NZB/debrid, OpenSubtitles, SSL, etc).
VORN_HTTP_ADDR=:8080
VORN_POSTGRES_DSN=postgres://vorn:vorn@localhost:5432/vorn?sslmode=disable
VORN_DRAGONFLY_ADDR=localhost:6379
VORN_CORS_ORIGIN=http://localhost:5173
"@ | Set-Content -Path $envFile -Encoding UTF8
} else {
    Write-Host "==> $envFile already exists, leaving it as-is"
}

# vornd reads its env file itself at startup (there's no native Windows
# service "EnvironmentFile" equivalent to systemd's), so this is passed as
# a startup argument rather than relying on the Windows service manager.
$binPath = "`"$InstallDir\vornd.exe`" -envfile `"$envFile`""

if (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue) {
    Write-Host "==> Updating existing service"
    sc.exe config $ServiceName binPath= $binPath | Out-Null
} else {
    Write-Host "==> Registering Windows service"
    sc.exe create $ServiceName binPath= $binPath start= auto DisplayName= "Vorn Media Server" | Out-Null
    sc.exe description $ServiceName "Self-hosted media server (see https://github.com/$Repo)" | Out-Null
}

Write-Host "==> Starting service"
Start-Service -Name $ServiceName

Write-Host ""
Write-Host "Installed and started. Edit $envFile and run 'Restart-Service vornd' to apply changes."
Write-Host "Logs: Event Viewer, or use Admin > Logs once it's running."
