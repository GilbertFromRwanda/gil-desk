# Local developer bootstrap (planner task F-22).
# Checks required toolchains and builds/tests the workspaces that exist today.

$ErrorActionPreference = "Stop"

function Test-Tool($name, $cmd) {
    try {
        & $cmd 2>&1 | Out-Null
        Write-Host "[ok] $name" -ForegroundColor Green
        return $true
    } catch {
        Write-Host "[missing] $name" -ForegroundColor Red
        return $false
    }
}

Write-Host "Checking toolchains..."
$ok = $true
$ok = (Test-Tool "cargo"  { cargo --version })  -and $ok
$ok = (Test-Tool "go"     { go version })       -and $ok
$ok = (Test-Tool "docker" { docker --version }) -and $ok
$ok = (Test-Tool "node"   { node --version })   -and $ok

if (-not $ok) {
    Write-Host "One or more required toolchains are missing. Install them before continuing." -ForegroundColor Yellow
    exit 1
}

Write-Host "`nBuilding Rust core..."
Push-Location core
cargo test --workspace
Pop-Location

Write-Host "`nBuilding Go backend..."
Push-Location backend
go build ./...
go test ./...
Pop-Location

Write-Host "`nBuilding C++ codec (in Docker)..."
docker build -t nexdesk-codec-build codec

Write-Host "`nVerifying Rust <-> C++ FFI actually links and calls (in Docker)..."
docker build -f core/Dockerfile -t nexdesk-core-ffi-verify .

Write-Host "`nBuilding + testing napi-rs native addon (Rust <-> Node)..."
Push-Location desktop/native
& ./build.ps1
node smoke-test.js
Pop-Location

Write-Host "`nBootstrap complete." -ForegroundColor Green
