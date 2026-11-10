[CmdletBinding()]
param([string]$OutputDirectory = 'dist/relay-windows-artifact')
$ErrorActionPreference = 'Stop'
$version = (Get-Content -Raw VERSION.json | ConvertFrom-Json).relay.version
$revision = (git rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0) { throw 'Unable to resolve source revision.' }
if (Test-Path -LiteralPath $OutputDirectory) { throw 'Refusing to replace an existing output directory.' }
$package = "herdr-link-relay-$version-windows-amd64-ci-$($revision.Substring(0,7))"
$stage = Join-Path $OutputDirectory $package
New-Item -ItemType Directory -Path $stage -Force | Out-Null
Copy-Item dist/windows/herdr-mobile-relay.exe $stage
Copy-Item deploy/windows-relay/*.ps1 $stage
Copy-Item LICENSE,VERSION.json $stage
Copy-Item docs/windows-relay.md (Join-Path $stage 'README.md')
Copy-Item -Recurse web $stage
$binary = Join-Path $stage 'herdr-mobile-relay.exe'
# PE machine, not merely a filename claiming Windows amd64.
$bytes = [IO.File]::ReadAllBytes($binary)
$pe = [BitConverter]::ToInt32($bytes, 0x3c)
if ($bytes[0] -ne 0x4d -or $bytes[1] -ne 0x5a -or [BitConverter]::ToUInt32($bytes,$pe) -ne 0x4550 -or [BitConverter]::ToUInt16($bytes,$pe+4) -ne 0x8664) { throw 'Expected Windows amd64 PE binary.' }
@{component='herdr-mobile-relay';version=$version;revision=$revision;target='windows/amd64';format='windows-relay-preview-v1';herdr_tested='0.9.5';binary_sha256=(Get-FileHash $binary -Algorithm SHA256).Hash.ToLowerInvariant();upstream_plugin_auto_update=$false;windows_service=$false} |
    ConvertTo-Json | Set-Content (Join-Path $stage 'BUILD-INFO.json') -Encoding utf8NoBOM
$zip = Join-Path $OutputDirectory "$package.zip"
Compress-Archive -Path $stage -DestinationPath $zip
"$((Get-FileHash $zip -Algorithm SHA256).Hash.ToLowerInvariant())  $package.zip" | Set-Content (Join-Path $OutputDirectory 'SHA256SUMS.txt') -Encoding ascii
Remove-Item -Recurse -LiteralPath $stage
Write-Host "CI preview package: $zip"
