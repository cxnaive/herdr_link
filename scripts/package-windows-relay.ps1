[CmdletBinding()]
param([string]$OutputDirectory = 'dist/relay-windows-artifact')
$ErrorActionPreference = 'Stop'
$version = (Get-Content -Raw VERSION.json | ConvertFrom-Json).relay.version
$revision = (git rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0) { throw 'Unable to resolve source revision.' }
$sourceStatus = @(git status --porcelain --untracked-files=normal)
if ($LASTEXITCODE -ne 0) { throw 'Unable to check source status.' }
if ($sourceStatus.Count) { throw 'Commit changes before packaging Windows Relay.' }
if (Test-Path -LiteralPath $OutputDirectory) { throw 'Refusing to replace an existing output directory.' }
$package = "herdr-link-relay-$version-windows-amd64"
$stage = Join-Path $OutputDirectory $package
New-Item -ItemType Directory -Path $stage -Force | Out-Null
Copy-Item dist/windows/herdr-mobile-relay.exe $stage
Copy-Item deploy/windows-relay/*.ps1 $stage
Copy-Item deploy/windows-relay/THIRD-PARTY-go-winio.txt $stage
Copy-Item LICENSE,VERSION.json $stage
Copy-Item docs/windows-relay.md (Join-Path $stage 'README.md')
Copy-Item -Recurse web $stage
$binary = Join-Path $stage 'herdr-mobile-relay.exe'
# PE machine, not merely a filename claiming Windows amd64.
$bytes = [IO.File]::ReadAllBytes($binary)
$pe = [BitConverter]::ToInt32($bytes, 0x3c)
if ($bytes[0] -ne 0x4d -or $bytes[1] -ne 0x5a -or [BitConverter]::ToUInt32($bytes,$pe) -ne 0x4550 -or [BitConverter]::ToUInt16($bytes,$pe+4) -ne 0x8664) { throw 'Expected Windows amd64 PE binary.' }
$stageRoot = (Resolve-Path -LiteralPath $stage).Path
$files = @(Get-ChildItem -LiteralPath $stageRoot -Recurse -File | Sort-Object FullName | ForEach-Object {
    @{file=[IO.Path]::GetRelativePath($stageRoot,$_.FullName).Replace('\','/');bytes=$_.Length;sha256=(Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash.ToLowerInvariant()}
})
@{component='herdr-mobile-relay';version=$version;revision=$revision;run_id=$env:GITHUB_RUN_ID;source_dirty=$false;target='windows/amd64';format='windows-relay-v1';signed=$false;herdr_tested='0.9.5';binary_sha256=(Get-FileHash $binary -Algorithm SHA256).Hash.ToLowerInvariant();files=$files;upstream_plugin_auto_update=$false;windows_service=$false} |
    ConvertTo-Json -Depth 5 | Set-Content (Join-Path $stage 'BUILD-INFO.json') -Encoding utf8NoBOM
$zip = Join-Path $OutputDirectory "$package.zip"
Compress-Archive -Path $stage -DestinationPath $zip
"$((Get-FileHash $zip -Algorithm SHA256).Hash.ToLowerInvariant())  $package.zip" | Set-Content (Join-Path $OutputDirectory 'SHA256SUMS.txt') -Encoding ascii
$outputRoot = (Resolve-Path -LiteralPath $OutputDirectory).Path
if ([IO.Path]::GetDirectoryName($stageRoot) -ne $outputRoot -or [IO.Path]::GetFileName($stageRoot) -ne $package) { throw 'Staging directory is outside the package output.' }
Remove-Item -Recurse -LiteralPath $stageRoot
Write-Host "Windows Relay package: $zip"
