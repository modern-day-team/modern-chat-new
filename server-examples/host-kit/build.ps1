$ErrorActionPreference = 'Stop'
Push-Location $PSScriptRoot
try {
    $env:GO111MODULE = 'off'
    $env:GOOS = 'wasip1'
    $env:GOARCH = 'wasm'
    gofmt -w main.go
    go build -o host-kit.wasm .
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    Write-Host "built $PSScriptRoot\host-kit.wasm"
} finally {
    Remove-Item Env:GO111MODULE -ErrorAction SilentlyContinue
    Remove-Item Env:GOOS -ErrorAction SilentlyContinue
    Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
    Pop-Location
}
