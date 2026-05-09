#Requires -Version 5.1
<#
.SYNOPSIS
    Thin wrapper around GNU Make for Windows.
    Resolves Git's usr/bin tools (bash, printf, mkdir, …) and prepends them to
    PATH so that GNU Make "Built for Windows32" can find them via CreateProcess.
.USAGE
    .\make.ps1 [target] [KEY=VALUE …]
    .\make.ps1 run
    .\make.ps1 build VERSION=1.2.0
#>

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# ── Validate required tools ───────────────────────────────────────────────────

function Resolve-Tool {
    param([string]$Name)
    $cmd = Get-Command $Name -ErrorAction SilentlyContinue
    if (-not $cmd) {
        Write-Error "Required tool not found: '$Name'. Please install it and ensure it is on PATH."
        exit 1
    }
    return $cmd.Path
}

$gitPath = Resolve-Tool 'git'
$null    = Resolve-Tool 'make'

# ── Prepend Git's Unix coreutils to PATH ─────────────────────────────────────
# git lives in …\cmd\git.exe; bash/printf/mkdir/… live in …\usr\bin\
# Using the actual git.exe location makes this work for any Git install
# (Scoop, winget, Git for Windows installer, etc.) without hardcoding paths.

$gitUsrBin = Join-Path (Split-Path (Split-Path $gitPath)) 'usr\bin'

if (-not (Test-Path $gitUsrBin)) {
    Write-Error "Could not locate Git's usr\bin at '$gitUsrBin'. Is this a standard Git for Windows installation?"
    exit 1
}

if ($env:PATH -notlike "*$gitUsrBin*") {
    $env:PATH = "$gitUsrBin;$env:PATH"
}

# ── Run make, forwarding all arguments ───────────────────────────────────────

& make @args
exit $LASTEXITCODE
