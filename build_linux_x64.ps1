<#
.SYNOPSIS
  Build Linux x64 binary for this Go project on any host with Go installed.

.DESCRIPTION
  Sets environment variables to target linux/amd64 and builds the binary named
  santak-ups-snmp-server.new in the current directory.

.USAGE
  From project root (PowerShell):
    .\build_linux_x64.ps1
#>

Param()

Write-Host "Building linux/amd64 binary..."

$env:GOOS = 'linux'
$env:GOARCH = 'amd64'
$env:CGO_ENABLED = '0'

try {
    go build -v -o santak-ups-snmp-server.new .
    Write-Host "Built santak-ups-snmp-server.new"
} catch {
    Write-Error "Build failed: $_"
    exit 1
}
