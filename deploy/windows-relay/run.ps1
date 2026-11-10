[CmdletBinding()]
param(
    [Parameter(Position=0)][ValidateSet('serve','gateway-join','tcp-pair','version')][string]$Action = 'serve',
    [Parameter(Position=1)][string]$Name = 'My client',
    [string]$RuntimeDirectory = (Join-Path $env:LOCALAPPDATA 'HerdrLink\relay')
)
$ErrorActionPreference = 'Stop'
$binary = Join-Path $PSScriptRoot 'herdr-mobile-relay.exe'
if ($Action -eq 'version') { & $binary version --json; exit $LASTEXITCODE }
$configPath = Join-Path $RuntimeDirectory 'windows-relay.json'
if (-not (Test-Path -LiteralPath $configPath -PathType Leaf)) { throw 'Run install.ps1 first.' }
$config = Get-Content -Raw -LiteralPath $configPath | ConvertFrom-Json
if (-not [IO.Path]::IsPathFullyQualified($RuntimeDirectory) -or -not [IO.Path]::IsPathFullyQualified($config.herdr_bin) -or -not [IO.Path]::IsPathFullyQualified($config.socket_path)) { throw 'Configuration paths must be absolute.' }
$env:HERDR_RELAY_ENV = Join-Path $RuntimeDirectory 'relay.env'
$env:HERDR_BIN = $config.herdr_bin
$env:HERDR_SOCKET_PATH = $config.socket_path
$env:HERDR_WEB_ROOT = Join-Path $PSScriptRoot 'web'
$env:HERDR_RELAY_PORT = [string]$config.port
$env:HERDR_RELAY_PLUGIN_PORT = [string]$config.plugin_port
$env:HERDR_RELAY_HOST = '127.0.0.1'
$env:HERDR_REACHABILITY_PORT_MAPPING = 'false'
# Gateway address/key comes from gateway-join's protected gateway-access.json.
if ($Action -eq 'tcp-pair') { & $binary tcp-pair $Name } else { & $binary $Action }
exit $LASTEXITCODE
