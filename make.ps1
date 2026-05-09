#Requires -Version 5.1
<#
.SYNOPSIS
    Thin wrapper around GNU Make for Windows.
    Resolves Git's usr/bin tools (bash, printf, mkdir, ...) and prepends them to
    PATH so that GNU Make "Built for Windows32" can find them via CreateProcess.
.USAGE
    .\make.ps1 [target] [KEY=VALUE ...]
    .\make.ps1 run
    .\make.ps1 build VERSION=1.2.0
#>

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# -- Validate required tools ---------------------------------------------------

$missing = @('make', 'zip', 'go', 'git', 'npm') | Where-Object { -not (Get-Command $_ -ErrorAction SilentlyContinue) }
if ($missing) {
    $scoopNames = $missing | ForEach-Object { if ($_ -eq 'npm') { 'nodejs' } else { $_ } }
    Write-Error "Missing required tool(s): $($missing -join ', ')`nInstall them with: scoop install $($scoopNames -join ' ')"
    exit 1
}

# -- Prepend Git's Unix coreutils to PATH -------------------------------------
# `git --exec-path` asks git itself where its core executables live.
# This resolves through any shim (Scoop, Chocolatey, winget, etc.) to the
# real installation — no fragile path arithmetic needed.

$gitExecPath = (& git --exec-path 2>$null) -replace '/', '\'
if (-not $gitExecPath) {
    Write-Error "Could not determine Git's exec path ('git --exec-path' failed). Is Git for Windows installed?"
    exit 1
}

# Walk up from the exec-path root to find usr\bin (layout varies by install).
$gitUsrBin = $null
$dir = $gitExecPath
while ($dir -and (Split-Path $dir) -ne $dir) {
    $candidate = Join-Path $dir 'usr\bin'
    if (Test-Path $candidate) { $gitUsrBin = $candidate; break }
    $dir = Split-Path $dir
}

if (-not $gitUsrBin) {
    Write-Error "Could not locate Git's usr\bin relative to exec path '$gitExecPath'. Is Git for Windows installed?"
    exit 1
}

if ($env:PATH -notlike "*$gitUsrBin*") {
    $env:PATH = "$gitUsrBin;$env:PATH"
}

# -- Run make, forwarding all arguments ---------------------------------------

& make @args
exit $LASTEXITCODE