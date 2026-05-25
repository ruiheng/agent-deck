[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [string]$Task = "help"
)

$ErrorActionPreference = "Stop"

$RepoRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$BuildDir = Join-Path $RepoRoot "build"
$BinaryName = "agent-deck.exe"
$NotifyFanoutBinaryName = "codex-notify-fanout.exe"
$GoExe = (Get-Command go -ErrorAction SilentlyContinue)?.Source
$HomeDir = if ($env:USERPROFILE) { $env:USERPROFILE } else { $HOME }
$LocalBin = Join-Path $HomeDir ".local\bin"
$TailwindVersion = "v4.2.2"
$TailwindExe = Join-Path $LocalBin "tailwindcss.exe"
$TailwindVersionStamp = Join-Path $LocalBin "tailwindcss.version"
$StylesSrc = Join-Path $RepoRoot "internal\web\static\styles.src.css"
$StylesOut = Join-Path $RepoRoot "internal\web\static\styles.css"
$TailwindAllowlist = Join-Path $RepoRoot "internal\web\static\.tailwind-allowlist.txt"
$GoToolchainVersion = "go1.25.10"

function Assert-Go {
    if (-not $GoExe) {
        throw "Go not found in PATH."
    }
}

function Invoke-Go {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Args
    )

    Assert-Go
    $old = $env:GOTOOLCHAIN
    try {
        $env:GOTOOLCHAIN = $GoToolchainVersion
        & $GoExe @Args
        if ($LASTEXITCODE -ne 0) {
            throw "go $($Args -join ' ') failed with exit code $LASTEXITCODE"
        }
    }
    finally {
        $env:GOTOOLCHAIN = $old
    }
}

function Get-VersionString {
    $version = "dev"
    try {
        $desc = git describe --tags --always --dirty 2>$null
        if ($LASTEXITCODE -eq 0 -and $desc) {
            $version = $desc.Trim()
            if ($version.StartsWith("v")) {
                $version = $version.Substring(1)
            }
        }
    }
    catch {
        $version = "dev"
    }
    return $version
}

function Get-TailwindAssetName {
    if (-not $IsWindows) {
        throw "This helper is intended for Windows. Use make on Unix-like systems."
    }

    switch ($env:PROCESSOR_ARCHITECTURE.ToLowerInvariant()) {
        "amd64" { return "tailwindcss-windows-x64.exe" }
        "arm64" { return "tailwindcss-windows-arm64.exe" }
        default { throw "Unsupported Windows architecture for Tailwind binary: $env:PROCESSOR_ARCHITECTURE" }
    }
}

function Ensure-Tailwind {
    New-Item -ItemType Directory -Force -Path $LocalBin | Out-Null

    $asset = Get-TailwindAssetName
    $downloadUrl = "https://github.com/tailwindlabs/tailwindcss/releases/download/$TailwindVersion/$asset"
    $needsDownload = $true

    if (Test-Path $TailwindExe) {
        if (Test-Path $TailwindVersionStamp) {
            $installedVersion = (Get-Content $TailwindVersionStamp -ErrorAction SilentlyContinue | Select-Object -First 1).Trim()
            if ($installedVersion -eq $TailwindVersion) {
                try {
                    & $TailwindExe --help *> $null
                    if ($LASTEXITCODE -eq 0) {
                        Write-Host "tailwindcss $TailwindVersion already installed at $TailwindExe"
                        $needsDownload = $false
                    }
                }
                catch {
                    Write-Warning "Existing tailwindcss.exe appears invalid; re-downloading."
                }
            }
        }
    }

    if (-not $needsDownload) {
        return
    }

    Write-Host "Downloading tailwindcss $TailwindVersion ($asset) ..."
    if (Test-Path $TailwindExe) {
        Remove-Item $TailwindExe -Force -ErrorAction SilentlyContinue
    }
    $curl = (Get-Command curl.exe -ErrorAction SilentlyContinue)?.Source
    if (-not $curl) {
        throw "curl.exe not found; cannot download Tailwind binary on Windows."
    }
    & $curl -L --fail --output $TailwindExe $downloadUrl
    if ($LASTEXITCODE -ne 0) {
        throw "curl.exe download failed for $downloadUrl"
    }

    try {
        & $TailwindExe --help *> $null
        if ($LASTEXITCODE -ne 0) {
            throw "Downloaded tailwindcss.exe did not run successfully"
        }
    }
    catch {
        Remove-Item $TailwindExe -Force -ErrorAction SilentlyContinue
        throw "Downloaded tailwindcss.exe is invalid on this machine: $($_.Exception.Message)"
    }

    Set-Content -Path $TailwindVersionStamp -Value $TailwindVersion -NoNewline
    Write-Host "Installed tailwindcss to $TailwindExe"
}

function Invoke-CssCompile {
    Ensure-Tailwind

    Write-Host "==> Compiling Tailwind CSS (targeted globs)"
    & $TailwindExe "-i" $StylesSrc "-o" $StylesOut "--minify"
    if ($LASTEXITCODE -ne 0) {
        throw "Tailwind compilation failed"
    }

    Write-Host "==> Brute-force globbing diff (Pitfall #1 gate)"
    $bruteSrc = Join-Path $RepoRoot "internal\web\static\.brute-tw.src.css"
    $bruteOut = Join-Path $env:TEMP "agent-deck-tw-brute.css"
    try {
        Copy-Item $StylesSrc $bruteSrc -Force
        Add-Content -Path $bruteSrc -Value @"

/* --- brute-force additional @source (Pitfall #1 gate) --- */
@source "./**/*.{js,mjs,html}";
@source not "./vendor/**";
@source not "./chart.umd.min.js";
@source not "./sw.js";
"@
        & $TailwindExe "-i" $bruteSrc "-o" $bruteOut "--minify"
        if ($LASTEXITCODE -ne 0) {
            throw "Tailwind brute-force compilation failed"
        }

        $stylesHash = (Get-FileHash $StylesOut -Algorithm SHA256).Hash
        $bruteHash = (Get-FileHash $bruteOut -Algorithm SHA256).Hash
        if ($stylesHash -ne $bruteHash) {
            if (Test-Path $TailwindAllowlist) {
                Write-Warning "Brute-force diff non-empty but allowlist file present; review manually."
            }
            else {
                throw "Brute-force @source diff non-empty (Pitfall #1). Add classes to internal/web/static/.tailwind-allowlist.txt or fix @source globs in styles.src.css."
            }
        }

        $gzipPath = Join-Path $env:TEMP "agent-deck-styles.css.gz"
        try {
            $input = [System.IO.File]::OpenRead($StylesOut)
            $output = [System.IO.File]::Create($gzipPath)
            $gzip = New-Object System.IO.Compression.GzipStream($output, [System.IO.Compression.CompressionLevel]::Optimal)
            $input.CopyTo($gzip)
            $gzip.Dispose()
            $input.Dispose()
            $output.Dispose()
            $size = (Get-Item $gzipPath).Length
        }
        finally {
            Remove-Item $gzipPath -ErrorAction SilentlyContinue
        }

        Write-Host "==> Compiled styles.css gzipped size: $size bytes"
        if ($size -gt 10240) {
            throw "gzipped size $size exceeds 10240-byte sanity ceiling"
        }
    }
    finally {
        Remove-Item $bruteSrc -ErrorAction SilentlyContinue
        Remove-Item $bruteOut -ErrorAction SilentlyContinue
    }
}

function Build-AgentDeck {
    Invoke-CssCompile
    New-Item -ItemType Directory -Force -Path $BuildDir | Out-Null
    $version = Get-VersionString
    Invoke-Go -Args @("build", "-ldflags", "-X main.Version=$version", "-o", (Join-Path $BuildDir $BinaryName), "./cmd/agent-deck")
}

function Build-NotifyFanout {
    New-Item -ItemType Directory -Force -Path $BuildDir | Out-Null
    Invoke-Go -Args @("build", "-o", (Join-Path $BuildDir $NotifyFanoutBinaryName), "./cmd/codex-notify-fanout")
}

function Show-Help {
    @"
Windows dev helper for Agent Deck

Usage:
  .\dev.cmd <task>
  pwsh -File .\dev.ps1 <task>

Tasks:
  help                       Show this help
  tools                      Install/download local dev tools needed on Windows
  css                        Build Tailwind CSS and run the brute-force diff gate
  css-verify                 Rebuild CSS and fail if committed output differs
  build                      Build ./build/agent-deck.exe
  build-notify-fanout        Build ./build/codex-notify-fanout.exe
  run                        Run the CLI directly
  test                       Run go test -race -v ./...
  fmt                        Run go fmt ./...
  lint                       Run golangci-lint
  clean                      Remove build artifacts and go clean
  install-user               Copy agent-deck.exe to %USERPROFILE%\.local\bin
  install-notify-fanout-user Copy codex-notify-fanout.exe to %USERPROFILE%\.local\bin
  dev                        Run with air (auto-installs if needed)
  ci                         Run lefthook pre-push checks
  release-local              Unsupported on Windows (use Unix-like environment)
"@ | Write-Host
}

switch ($Task.ToLowerInvariant()) {
    "help" {
        Show-Help
    }
    "tools" {
        Ensure-Tailwind
    }
    "css" {
        Invoke-CssCompile
    }
    "css-verify" {
        Invoke-CssCompile
        git diff --exit-code -- $StylesOut
        if ($LASTEXITCODE -ne 0) {
            throw "internal/web/static/styles.css drifted from generated output. Run '.\dev.cmd css' and commit."
        }
    }
    "build" {
        Build-AgentDeck
    }
    "build-notify-fanout" {
        Build-NotifyFanout
    }
    "run" {
        Invoke-Go -Args @("run", "./cmd/agent-deck")
    }
    "test" {
        Invoke-Go -Args @("test", "-race", "-v", "./...")
    }
    "fmt" {
        Invoke-Go -Args @("fmt", "./...")
    }
    "lint" {
        $golangci = (Get-Command golangci-lint -ErrorAction SilentlyContinue)?.Source
        if (-not $golangci) {
            Invoke-Go -Args @("install", "github.com/golangci/golangci-lint/cmd/golangci-lint@latest")
            $golangci = (Get-Command golangci-lint -ErrorAction SilentlyContinue)?.Source
        }
        if (-not $golangci) {
            throw "golangci-lint not found after install attempt"
        }
        & $golangci run
        if ($LASTEXITCODE -ne 0) {
            throw "golangci-lint failed"
        }
    }
    "clean" {
        Remove-Item $BuildDir -Recurse -Force -ErrorAction SilentlyContinue
        Invoke-Go -Args @("clean")
    }
    "install-user" {
        Build-AgentDeck
        New-Item -ItemType Directory -Force -Path $LocalBin | Out-Null
        Copy-Item (Join-Path $BuildDir $BinaryName) (Join-Path $LocalBin $BinaryName) -Force
        Write-Host "✅ Installed to $(Join-Path $LocalBin $BinaryName)"
    }
    "install-notify-fanout-user" {
        Build-NotifyFanout
        New-Item -ItemType Directory -Force -Path $LocalBin | Out-Null
        Copy-Item (Join-Path $BuildDir $NotifyFanoutBinaryName) (Join-Path $LocalBin $NotifyFanoutBinaryName) -Force
        Write-Host "✅ Installed to $(Join-Path $LocalBin $NotifyFanoutBinaryName)"
    }
    "dev" {
        $air = (Get-Command air -ErrorAction SilentlyContinue)?.Source
        if (-not $air) {
            Invoke-Go -Args @("install", "github.com/cosmtrek/air@latest")
            $air = (Get-Command air -ErrorAction SilentlyContinue)?.Source
        }
        if (-not $air) {
            throw "air not found after install attempt"
        }
        & $air
        if ($LASTEXITCODE -ne 0) {
            throw "air failed"
        }
    }
    "ci" {
        $lefthook = (Get-Command lefthook -ErrorAction SilentlyContinue)?.Source
        if (-not $lefthook) {
            throw "lefthook not found. Install it first, then run '.\\dev.cmd ci'."
        }
        & $lefthook run pre-push --force --no-auto-install
        if ($LASTEXITCODE -ne 0) {
            throw "lefthook pre-push checks failed"
        }
    }
    "release-local" {
        throw "release-local is not supported by dev.ps1 on Windows. Use the Unix Makefile path instead."
    }
    default {
        throw "Unknown task '$Task'. Run '.\\dev.cmd help' for usage."
    }
}
