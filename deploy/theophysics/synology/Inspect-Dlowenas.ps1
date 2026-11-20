# Run in Windows PowerShell from this directory. No password is saved by this script.
# Uses Windows OpenSSH and its normal host-key verification and login prompts.
[CmdletBinding()]
param(
    [string]$NasHost = 'dlowenas',
    [Parameter(Mandatory = $true)]
    [string]$SshUser,
    [ValidateRange(1, 65535)]
    [int]$Port = 22,
    [string]$ReportPath = (Join-Path $PWD 'dlowenas-inspection.txt')
)
$ErrorActionPreference = 'Stop'
if ($NasHost -notmatch '^[A-Za-z0-9][A-Za-z0-9.:-]*$' -or $SshUser -notmatch '^[A-Za-z0-9_][A-Za-z0-9_.-]*$') {
    throw 'Use a plain NAS hostname/IP and DSM username, without shell arguments.'
}
if (-not (Get-Command ssh.exe -ErrorAction SilentlyContinue)) {
    throw 'Windows OpenSSH Client is not installed. Use the DSM Task Scheduler instructions instead.'
}
$scriptPath = Join-Path $PSScriptRoot 'inspect-nas.sh'
if (-not (Test-Path -LiteralPath $scriptPath)) { throw "Missing $scriptPath" }
if (Test-Path -LiteralPath $ReportPath) { throw 'The report already exists. Choose another ReportPath to preserve it.' }
# ASCII shell source avoids PowerShell 5.1's legacy native-pipe encoding issues.
$source = [IO.File]::ReadAllText($scriptPath).Replace("`r`n", "`n")
$lines = $source | & ssh.exe -T -p $Port -l $SshUser $NasHost 'sh -s'
$status = $LASTEXITCODE
if ($status -ne 0) { throw "SSH inspection failed (exit $status). No report was saved." }
if (($lines -join "`n") -notmatch 'THEOPHYSICS_NAS_INSPECTION_VERSION=1') {
    throw 'The expected inspection report was not returned. No report was saved.'
}
$stream = [IO.File]::Open($ReportPath, [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::None)
try {
    $writer = [IO.StreamWriter]::new($stream, [Text.UTF8Encoding]::new($false))
    try { foreach ($line in $lines) { $writer.WriteLine([string]$line) } } finally { $writer.Dispose() }
} finally { $stream.Dispose() }
Write-Host "Read-only inspection saved to $ReportPath"
Write-Host 'Review the report and share it so we can choose storage paths. Do not share credentials.'
