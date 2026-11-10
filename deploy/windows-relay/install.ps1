# User-local Windows Relay preview setup. Does not start/restart Herdr or Relay.
[CmdletBinding()]
param([string]$RuntimeDirectory = (Join-Path $env:LOCALAPPDATA 'HerdrLink\relay'))
$ErrorActionPreference = 'Stop'
if (-not [IO.Path]::IsPathFullyQualified($RuntimeDirectory)) { throw 'Use an absolute runtime directory.' }
$RuntimeDirectory = [IO.Path]::GetFullPath($RuntimeDirectory)
# Refuse junctions/symlinks in the private configuration path.
for ($p = $RuntimeDirectory; $p; $p = [IO.Path]::GetDirectoryName($p)) {
    if ((Test-Path -LiteralPath $p) -and ((Get-Item -LiteralPath $p -Force).Attributes -band [IO.FileAttributes]::ReparsePoint)) { throw "Reparse path is not allowed: $p" }
}
[IO.Directory]::CreateDirectory($RuntimeDirectory) | Out-Null
$sid = [Security.Principal.WindowsIdentity]::GetCurrent().User
$acl = [Security.AccessControl.DirectorySecurity]::new()
$acl.SetOwner($sid)
$acl.SetAccessRuleProtection($true, $false)
foreach ($id in @($sid.Value, 'S-1-5-18', 'S-1-5-32-544')) {
    $identity = [Security.Principal.SecurityIdentifier]::new($id)
    $rule = [Security.AccessControl.FileSystemAccessRule]::new($identity, 'FullControl', 'ContainerInherit,ObjectInherit', 'None', 'Allow')
    $acl.AddAccessRule($rule)
}
Set-Acl -LiteralPath $RuntimeDirectory -AclObject $acl
$config = Join-Path $RuntimeDirectory 'windows-relay.json'
if (-not (Test-Path -LiteralPath $config)) {
    $herdr = (Get-Command herdr.exe -CommandType Application -ErrorAction Stop).Source
    @{ herdr_bin = $herdr; socket_path = (Join-Path $env:APPDATA 'herdr\herdr.sock'); port = 8375; plugin_port = 8376 } |
        ConvertTo-Json | Set-Content -LiteralPath $config -Encoding utf8NoBOM
}
Write-Host "Private configuration: $config (existing identity and pairing preserved)"
Write-Host 'Next: ./run.ps1 gateway-join; paste a Gateway computer invitation when prompted.'
Write-Host 'Then: ./run.ps1 serve'
Write-Host 'For another terminal: ./run.ps1 tcp-pair "My phone"'
Write-Host 'Keep Herdr, Relay and agents under the same ordinary Windows account.'
