# Builds the napi-rs native addon and stages it as index.node (planner F-18/F-19).
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

cargo build
Copy-Item -Force target/debug/nexdesk_native.dll index.node

Write-Host "Built index.node. Run 'node smoke-test.js' to verify." -ForegroundColor Green
