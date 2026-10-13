# Throwaway PostgreSQL cluster for local DB tests (WH-B, docs/dev/parallel-packages.md).
#
# The shared dev cluster (localhost:5432, %LOCALAPPDATA%\agrigreen-pg) is used by several agents at once and
# sometimes crashes or sits in recovery. This script creates a PRIVATE cluster in a temp dir on a free port,
# so a test run never depends on (and never touches) the shared one.
#
# Usage (PowerShell, from the repo root):
#   .\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 ./internal/platform/party/...
#       init + start, run the command with AG_TEST_PG_ADMIN_URL set, then stop and delete. Exit code = command's.
#   . .\scripts\pg-ephemeral.ps1 start      (dot-source: sets AG_TEST_PG_ADMIN_URL in THIS session)
#   . .\scripts\pg-ephemeral.ps1 stop       (stops and deletes the cluster started by 'start' in this session)
#   .\scripts\pg-ephemeral.ps1 status
#
# Safety rules (enforced):
#   - never uses port 5432 and never calls pg_ctl on a data dir it did not create (marker file check);
#   - the superuser password is random, lives only in the env var of this process/session, never printed;
#   - listens on 127.0.0.1 only.
# Binaries: initdb/pg_ctl/psql from AG_PG_BIN (default %LOCALAPPDATA%\Programs\pgsql\bin, same install as the shared
# cluster, so postgis and btree_gist are available). This file must stay ASCII (Windows PowerShell 5.1).

# No param() block on purpose: every argument after the action goes to the command untouched, so go test flags
# such as -p 1 or -timeout 55m are not taken as script parameters. Options come from environment variables:
#   AG_PG_BIN              directory with initdb.exe/pg_ctl.exe/psql.exe (default %LOCALAPPDATA%\Programs\pgsql\bin)
#   AG_EPHEMERAL_PG_PORT   fixed port (default: a free port chosen by the OS; 5432 is refused)
$Action = 'status'
$Command = @()
if ($args.Count -gt 0) { $Action = [string]$args[0] }
if ($args.Count -gt 1) { $Command = @($args[1..($args.Count - 1)] | ForEach-Object { [string]$_ }) }
if (@('run', 'start', 'stop', 'status') -notcontains $Action) { throw "pg-ephemeral: unknown action '$Action' (run, start, stop, status)" }
$PgBin = "$env:LOCALAPPDATA\Programs\pgsql\bin"
if ($env:AG_PG_BIN) { $PgBin = $env:AG_PG_BIN }
$Port = 0
if ($env:AG_EPHEMERAL_PG_PORT) { $Port = [int]$env:AG_EPHEMERAL_PG_PORT }

# ErrorActionPreference 'Stop' is set inside the functions (function scope) so a dot-sourced start/stop does not change
# the caller's session preference.
$Marker = '.agrigreen-ephemeral'
$SharedPort = 5432

function Get-FreePort {
  $l = New-Object System.Net.Sockets.TcpListener([System.Net.IPAddress]::Loopback, 0)
  $l.Start()
  $p = $l.LocalEndpoint.Port
  $l.Stop()
  return $p
}

function New-Secret {
  $b = New-Object byte[] 24
  [System.Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($b)
  return ([System.BitConverter]::ToString($b) -replace '-', '').ToLowerInvariant()
}

# Native tools write progress to stderr; under 'Stop' PowerShell 5.1 turns redirected stderr into a terminating
# error. Run them with 'Continue' and judge by exit code only.
function Invoke-Logged([string]$exe, [string[]]$argv, [string]$log) {
  $old = $ErrorActionPreference
  $ErrorActionPreference = 'Continue'
  try { & $exe @argv *> $log } finally { $ErrorActionPreference = $old }
  return $LASTEXITCODE
}

function Assert-Ephemeral([string]$dir) {
  if (-not $dir -or -not (Test-Path (Join-Path $dir $Marker))) {
    throw "pg-ephemeral: '$dir' is not a cluster created by this script - refusing to touch it"
  }
}

function Start-Ephemeral {
  $ErrorActionPreference = 'Stop'
  foreach ($exe in 'initdb.exe', 'pg_ctl.exe', 'psql.exe') {
    if (-not (Test-Path (Join-Path $PgBin $exe))) { throw "pg-ephemeral: $exe not found in $PgBin (see docs/dev-setup.md)" }
  }
  $p = $Port
  if ($p -eq 0) { $p = Get-FreePort }
  if ($p -eq $SharedPort) { throw "pg-ephemeral: port $SharedPort belongs to the shared cluster - choose another" }

  $root = Join-Path ([System.IO.Path]::GetTempPath()) ("agrigreen-pg-ephemeral-" + [System.Guid]::NewGuid().ToString('N').Substring(0, 12))
  $data = Join-Path $root 'data'
  New-Item -ItemType Directory -Path $root | Out-Null
  Set-Content -Path (Join-Path $root $Marker) -Value "created by scripts/pg-ephemeral.ps1 port=$p" -Encoding ascii

  $secret = New-Secret
  $pwFile = Join-Path $root 'pw.tmp'
  [System.IO.File]::WriteAllText($pwFile, $secret)
  try {
    $rc = Invoke-Logged (Join-Path $PgBin 'initdb.exe') @('-D', $data, '-U', 'postgres', "--pwfile=$pwFile", '-A', 'scram-sha-256', '-E', 'UTF8', '--locale=C', '--no-instructions') (Join-Path $root 'initdb.log')
    if ($rc -ne 0) { throw "pg-ephemeral: initdb failed, see $root\initdb.log" }
  } finally {
    Remove-Item -LiteralPath $pwFile -Force -ErrorAction SilentlyContinue
  }

  # Throwaway settings: speed over durability; same lock budget as the shared cluster.
  $conf = @(
    '',
    '# pg-ephemeral (throwaway cluster)',
    "port = $p",
    "listen_addresses = '127.0.0.1'",
    'max_connections = 200',
    'max_locks_per_transaction = 256',
    'fsync = off',
    'synchronous_commit = off',
    'full_page_writes = off'
  )
  Add-Content -Path (Join-Path $data 'postgresql.conf') -Value $conf -Encoding ascii

  # Start-Process with file redirection: the postgres child inherits file handles, not a pipe, so waiting for
  # pg_ctl does not wait for the server to exit. pg_ctl -w returns once the server accepts connections.
  $startArgs = "-D `"$data`" -l `"$(Join-Path $root 'postgres.log')`" -w -t 60 start"
  $pr = Start-Process -FilePath (Join-Path $PgBin 'pg_ctl.exe') -ArgumentList $startArgs -NoNewWindow -PassThru `
    -RedirectStandardOutput (Join-Path $root 'pg_ctl.out') -RedirectStandardError (Join-Path $root 'pg_ctl.err')
  $null = $pr.Handle
  $pr.WaitForExit()
  if ($pr.ExitCode -ne 0) { throw "pg-ephemeral: pg_ctl start failed, see $root\postgres.log" }

  # Bootstrap check: extensions used by migrations must be installable (roles and databases are created per test
  # package by pgtest / dbadmin.Bootstrap).
  $env:PGPASSWORD = $secret
  try {
    $n = & (Join-Path $PgBin 'psql.exe') -h 127.0.0.1 -p $p -U postgres -d postgres -tAc "SELECT count(*) FROM pg_available_extensions WHERE name IN ('postgis','btree_gist')"
  } finally {
    Remove-Item Env:\PGPASSWORD -ErrorAction SilentlyContinue
  }
  if ("$n".Trim() -ne '2') {
    Write-Warning "pg-ephemeral: postgis/btree_gist not available in $PgBin - migrations using them will fail"
  }

  $env:AG_EPHEMERAL_PG_DIR = $root
  $env:AG_TEST_PG_ADMIN_URL = "postgres://postgres:$secret@127.0.0.1:$p/postgres?sslmode=disable"
  if (-not $env:AG_DB_APP_PASSWORD) { $env:AG_DB_APP_PASSWORD = 'agtest-local-only' }
  # A private cluster is either up or not: do not wait for it like for the shared one.
  if (-not $env:AG_TEST_PG_WAIT) { $env:AG_TEST_PG_WAIT = '10' }
  $goBin = "$env:LOCALAPPDATA\Programs\go\bin"
  if ((Test-Path $goBin) -and ($env:Path -notlike "*$goBin*")) { $env:Path = "$goBin;$env:Path" }
  Write-Host "pg-ephemeral: cluster on 127.0.0.1:$p in $root (AG_TEST_PG_ADMIN_URL set, password not shown)"
}

function Stop-Ephemeral {
  $ErrorActionPreference = 'Stop'
  $root = $env:AG_EPHEMERAL_PG_DIR
  if (-not $root) { Write-Host 'pg-ephemeral: nothing to stop in this session'; return }
  Assert-Ephemeral $root
  $data = Join-Path $root 'data'
  if (Test-Path (Join-Path $data 'postmaster.pid')) {
    $null = Invoke-Logged (Join-Path $PgBin 'pg_ctl.exe') @('-D', $data, '-m', 'immediate', '-w', '-t', '60', 'stop') (Join-Path ([System.IO.Path]::GetTempPath()) 'agrigreen-pg-ephemeral-stop.log')
  }
  Remove-Item -LiteralPath $root -Recurse -Force -ErrorAction SilentlyContinue
  if (Test-Path $root) { Write-Warning "pg-ephemeral: could not delete $root (files in use?)" }
  Remove-Item Env:\AG_EPHEMERAL_PG_DIR, Env:\AG_TEST_PG_ADMIN_URL -ErrorAction SilentlyContinue
  Write-Host "pg-ephemeral: stopped and removed $root"
}

switch ($Action) {
  'start' { Start-Ephemeral }
  'stop' { Stop-Ephemeral }
  'status' {
    if ($env:AG_EPHEMERAL_PG_DIR) { Write-Host "pg-ephemeral: active in this session: $env:AG_EPHEMERAL_PG_DIR" }
    else { Write-Host 'pg-ephemeral: no cluster in this session' }
  }
  'run' {
    if (-not $Command -or $Command.Count -eq 0) { throw 'pg-ephemeral run: give the command to run, e.g. run go test ./...' }
    $saved = @{ url = $env:AG_TEST_PG_ADMIN_URL; dir = $env:AG_EPHEMERAL_PG_DIR; wait = $env:AG_TEST_PG_WAIT }
    $code = 1
    try {
      Start-Ephemeral
      $exe = $Command[0]
      $rest = @()
      if ($Command.Count -gt 1) { $rest = $Command[1..($Command.Count - 1)] }
      & $exe @rest
      $code = $LASTEXITCODE
    } finally {
      Stop-Ephemeral
      if ($saved.url) { $env:AG_TEST_PG_ADMIN_URL = $saved.url }
      if ($saved.dir) { $env:AG_EPHEMERAL_PG_DIR = $saved.dir }
      if ($saved.wait) { $env:AG_TEST_PG_WAIT = $saved.wait } else { Remove-Item Env:\AG_TEST_PG_WAIT -ErrorAction SilentlyContinue }
    }
    exit $code
  }
}
