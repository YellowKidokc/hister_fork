$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot
py -3 -m venv venv
if ($LASTEXITCODE -ne 0) { throw 'Python environment creation failed' }
& "$PSScriptRoot\venv\Scripts\python.exe" -m pip install -r requirements.txt
if ($LASTEXITCODE -ne 0) { throw 'Dependency installation failed' }
