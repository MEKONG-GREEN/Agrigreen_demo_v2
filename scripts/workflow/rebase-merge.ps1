param(
    [Parameter(Mandatory)][Alias('Pkg')][string]$Worktree,   # thư mục dưới AG_WORKTREE_ROOT
    [Parameter(Mandatory)][string]$Ticket,                     # mã gói, vd P0-WEB-IDEMKEY -> nhánh PR pr/<Ticket>
    [Parameter(Mandatory)][int]$Issue,
    [Parameter(Mandatory)][ValidateSet('claude', 'codex')][string]$Agent,   # bên gộp; cần Review OK của bên kia
    [string]$IssueTitle = '',
    [string[]]$Tests = @(),
    [string]$GateRun = 'SchemaIsolation|RLSExplain|ModuleGroup|LoginRoles|NoCrossModule|LedgerAddColumn',
    [int]$ChecksTimeoutMin = 0,          # 0 -> AG_PR_CHECKS_TIMEOUT_MIN hoặc 90
    [string]$AllowFailingChecks = '',    # regex tên check được phép đỏ khi repo CHƯA có check bắt buộc (chỉ cảnh báo)
    [switch]$DryRun
)
# Rebase nhánh gói lên origin/dev, gỡ xung đột tài liệu (giữ cả hai) và tệp sinh (lấy dev rồi sinh lại), kiểm cổng review
# chéo, chạy cổng + kiểm thử, rồi đẩy pr/<Ticket>, tạo/dùng lại PR vào dev, chờ check, gộp bằng merge commit đúng SHA đầu.
# Không còn đường push thẳng dev. Mã thoát: 0 gộp xong; 1 cổng/kiểm thử/regen; 2 xung đột mã; 4 review; 5 check PR; 6 gộp PR.
# -DryRun: rebase thử trong worktree tạm (không đụng nhánh gói), chạy thật cổng review (chỉ đọc), in mọi lệnh còn lại.
$ErrorActionPreference = 'Continue'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$sp = Split-Path -Parent $MyInvocation.MyCommand.Path
. (Join-Path $sp 'workflow-lib.ps1')
$script:AgDryRun = [bool]$DryRun
if (-not $DryRun -and -not $env:AG_INTEGRATION_LOCK) { throw 'Run through scripts/workflow/ticket.ps1 merge (holds origin lock/integration); direct runs are refused' }
$ctx = Get-AgContext $sp
$gh = $ctx.GhRepo
$w = Join-Path $ctx.WorktreeRoot $Worktree
$agBranch = git -C $w symbolic-ref --short HEAD
if ($LASTEXITCODE -ne 0 -or -not $agBranch -or $agBranch -in @('main', 'dev')) { throw 'Expected a package branch, refusing main/dev or detached HEAD' }
$env:GIT_EDITOR = 'true'
git -C $w fetch -q origin +refs/heads/dev:refs/remotes/origin/dev
if ($LASTEXITCODE -ne 0) { throw 'FETCH FAIL' }
if (git -C $w status --porcelain) { throw 'Worktree has uncommitted changes; resolve them explicitly before integration' }
$oldHead = "$(git -C $w rev-parse HEAD)".Trim()
$prBranch = "pr/$Ticket"
"package $Ticket (#$Issue) branch $agBranch head $($oldHead.Substring(0,12)) -> PR head $prBranch, base dev $("$(git -C $w rev-parse --short origin/dev)".Trim())"

$dryWt = $null
$dryBlocked = $false
if ($DryRun) {
    # Rebase thử trên bản sao tách rời để lấy SHA đầu PR thật mà không sửa nhánh/worktree của gói.
    $dryWt = Join-Path $env:TEMP ("agwf-dry-{0}-{1}" -f $Worktree, [IO.Path]::GetRandomFileName().Replace('.', ''))
    git -C $w worktree add -q --detach $dryWt $oldHead 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { throw "cannot create dry-run worktree $dryWt" }
    "[dry-run] rebase simulated in temporary worktree $dryWt (package worktree untouched)"
    $w = $dryWt
}
try {
    git -C $w merge-base --is-ancestor origin/dev HEAD
    if ($LASTEXITCODE -ne 0) {
        git -C $w rebase --rebase-merges -q origin/dev 2>&1 | Select-Object -Last 1
    }
    while ($LASTEXITCODE -ne 0) {
        $c = @(git -C $w diff --name-only --diff-filter=U)
        "conflicts: $c"
        if (-not $c.Count) {
            # A commit that only touched generated files becomes empty once dev's version is kept: skip it.
            if ((Test-Path (Join-Path "$(git -C $w rev-parse --absolute-git-dir)".Trim() 'rebase-merge')) -and -not (git -C $w status --porcelain)) {
                git -C $w rebase --skip 2>&1 | Select-Object -Last 1; continue
            }
            throw 'Rebase failed without unresolved files; inspect worktree before continuing'
        }
        # Generated outputs (modreg, module menus/mocks/OpenAPI, TS client, Go menu golden) are taken from dev here and
        # rebuilt from their sources after the rebase (see "regenerate" below); never hand-merge them.
        $gen = @($c | Where-Object { Test-AgGeneratedPath $_ })
        $doc = @($c | Where-Object { $_ -match '\.md$' })
        $other = @($c | Where-Object { -not (Test-AgGeneratedPath $_) -and $_ -notmatch '\.md$' })
        if ($other.Count) {
            "CODE CONFLICT - stop: $other"
            if ($DryRun) { git -C $w rebase --abort 2>&1 | Out-Null }
            exit 2
        }
        if ($doc.Count) { & "$sp\union.ps1" -Files ($doc | ForEach-Object { Join-Path $w $_ }); git -C $w add $doc }
        if ($gen.Count) {
            foreach ($g in $gen) { git -C $w checkout --ours -- $g }
            git -C $w add $gen
        }
        git -C $w rebase --continue 2>&1 | Select-Object -Last 1
    }
    $regen = @(
        'go run ./tools/modgen',
        'apps/web-ops: npm ci (if no node_modules); npm run gen:api',
        'AG_UPDATE_WEB_FIXTURES=1 go test -count=1 ./internal/platform/party/handler -run TestWebMenuFixture',
        'git add -A; git commit "<Ticket>: sinh lại tệp sinh ..." (if anything changed)')
    if ($DryRun) {
        $regen | ForEach-Object { "[dry-run] regenerate: $_" }
    } else {
        Push-Location $w
        go run ./tools/modgen
        # regenerate: rebuild generated outputs from sources so the merged tree is consistent (menus/mocks/OpenAPI, TS client,
        # Go web-menu golden). Always runs: cheap, and catches a branch whose generated files predate dev's newer sources.
        Push-Location (Join-Path $w 'apps\web-ops')
        if (-not (Test-Path node_modules)) { npm ci --no-audit --no-fund 2>&1 | Select-Object -Last 2 }
        npm run gen:api 2>&1 | Select-Object -Last 2
        if ($LASTEXITCODE -ne 0) { Pop-Location; Pop-Location; 'REGEN FAIL (gen:api)'; exit 1 }
        Pop-Location
        if (-not $env:AG_TEST_PG_ADMIN_URL) { . .\scripts\dev-env.ps1 | Out-Null }
        $env:AG_UPDATE_WEB_FIXTURES = '1'
        go test -count=1 ./internal/platform/party/handler -run TestWebMenuFixture 2>&1 | Select-String '^(ok|FAIL|---)'
        $fx = $LASTEXITCODE
        Remove-Item Env:\AG_UPDATE_WEB_FIXTURES
        if ($fx -ne 0) { Pop-Location; 'REGEN FAIL (web menu fixture)'; exit 1 }
        if (git status --porcelain) { git add -A; git commit -q -m "${Ticket}: sinh lại tệp sinh (modreg, menu/mock/OpenAPI, client TS, golden menu) sau rebase`n`nCo-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>" }
        Pop-Location
    }
    $head = "$(git -C $w rev-parse HEAD)".Trim()
    "PR head after rebase/regeneration: $head"

    # --- Cổng review chéo: trước mọi cổng tốn thời gian và mọi thao tác từ xa -------------------------------------------
    $openPr = Get-AgOpenPr $gh $prBranch
    $comments = Get-AgThreadComments $gh $Issue $(if ($openPr) { "$($openPr.number)" } else { '' })
    $verdicts = @(Get-AgReviewVerdicts $comments)
    $gate = Resolve-AgReviewGate $verdicts $Agent $head $w 'origin/dev' $Ticket
    $gate.Message
    if (-not $gate.Ok) {
        if ($DryRun) { $dryBlocked = $true; '[dry-run] a real merge would STOP here (exit 4); continuing to print the plan' } else { exit 4 }
    }

    # --- Cổng cục bộ (giữ nguyên như quy trình cũ) ----------------------------------------------------------------------
    $gateCmds = @('go build ./...', 'go vet ./...', 'go run ./tools/modgen -check', 'go run ./tools/migratecheck -base origin/dev migrations',
        'go run ./tools/archlint ./...', 'go run ./tools/knboundary -base origin/dev (if present)')
    if ($Tests.Count) { $gateCmds += "go test -count=1 -p 1 -timeout 55m $($Tests -join ' ')" }
    $gateCmds += "go test -count=1 -p 1 -timeout 55m -run '$GateRun' ./migrations/"
    $testSummary = @()
    if ($DryRun) {
        $gateCmds | ForEach-Object { "[dry-run] gate: $_" }
        $gateLine = 'build=? vet=? modgen=? migratecheck=? archlint=? knboundary=? (dry-run: not executed)'
    } else {
        Push-Location $w
        go build ./...; $b = $LASTEXITCODE
        go vet ./...; $v = $LASTEXITCODE
        go run ./tools/modgen -check; $m = $LASTEXITCODE
        go run ./tools/migratecheck -base origin/dev migrations; $mc = $LASTEXITCODE
        go run ./tools/archlint ./... 2>&1 | Select-Object -Last 1; $a = $LASTEXITCODE
        $agBoundary = 0
        if (Test-Path -LiteralPath (Join-Path $w 'tools/knboundary/main.go')) {
            go run ./tools/knboundary -base origin/dev
            $agBoundary = $LASTEXITCODE
        }
        $gateLine = "build=$b vet=$v modgen=$m migratecheck=$mc archlint=$a knboundary=$agBoundary"
        $gateLine
        if ($b + $v + $m + $mc + $a + $agBoundary -ne 0) { Pop-Location; exit 1 }
        if (-not $env:AG_TEST_PG_ADMIN_URL) { . .\scripts\dev-env.ps1 | Out-Null }
        if (-not $env:AG_TEST_PG_ADMIN_URL) { throw 'DB configuration missing - refusing skipped DB tests' }
        if ($Tests.Count) {
            $o = @(go test -count=1 -p 1 -timeout 55m @Tests 2>&1 | Select-String '^(ok|FAIL|---|panic)'); $rcT = $LASTEXITCODE
            $o; $testSummary += $o | ForEach-Object { "$_" }
            if ($rcT -ne 0) { 'TEST FAIL'; Pop-Location; exit 1 }
        }
        $o = @(go test -count=1 -p 1 -timeout 55m -run $GateRun ./migrations/ 2>&1 | Select-String '^(ok|FAIL|---|panic|INFRA)'); $rcG = $LASTEXITCODE
        $o; $testSummary += $o | ForEach-Object { "$_" }
        if ($rcG -ne 0) { 'GATE FAIL'; Pop-Location; exit 1 }
        Pop-Location
    }

    # --- Pull Request vào dev ---------------------------------------------------------------------------------------------
    git -C $w fetch -q origin +refs/heads/dev:refs/remotes/origin/dev
    git -C $w merge-base --is-ancestor origin/dev $head
    if ($LASTEXITCODE -ne 0) { 'NOT UP TO DATE: origin/dev moved while holding the lock'; if (-not $DryRun) { exit 1 } }
    $lease = "$((git -C $w ls-remote origin "refs/heads/$prBranch") -split '\s+' | Select-Object -First 1)".Trim()
    Invoke-AgMut git @('-C', $w, 'push', 'origin', "--force-with-lease=refs/heads/${prBranch}:$lease", "${head}:refs/heads/$prBranch")
    if ($LASTEXITCODE -ne 0) { throw "PUSH $prBranch FAIL - worktree retained" }

    $title = if ($IssueTitle) { $IssueTitle -replace '^\[[^\]]+\]\s*', '' } else { $Ticket }
    $body = @(
        "Refs #$Issue", '',
        "Gói **$Ticket** — nhánh ``$agBranch`` rebase lên ``origin/dev``, đầu PR ``$head``.", '',
        '### Cổng cục bộ (scripts/workflow/rebase-merge.ps1)', '', '```', $gateLine) + $testSummary + @('```', '',
        '### Review chéo', '',
        $(if ($gate.Review) { "$($gate.Review.Line) — $($gate.Review.Url) (khớp: $($gate.Mode))" } else { '(chưa có — dry-run)' }), '',
        '_PR do `ticket.ps1 merge` tạo; gộp bằng merge commit với `--match-head-commit`. Issue được script đóng sau khi gộp._')
    $bodyFile = Join-Path $env:TEMP "agwf-pr-$Worktree.md"
    [IO.File]::WriteAllText($bodyFile, ($body -join "`n"), (New-Object System.Text.UTF8Encoding($false)))
    if ($DryRun) { '[dry-run] PR body:'; $body | ForEach-Object { "    $_" } }
    $prTitle = "[$Ticket] $title"
    if ($openPr) {
        $pr = "$($openPr.number)"
        Invoke-AgMut gh @('pr', 'edit', $pr, '-R', $gh, '--title', $prTitle, '--body-file', $bodyFile)
    } else {
        $out = @(Invoke-AgMut gh @('pr', 'create', '-R', $gh, '--base', 'dev', '--head', $prBranch, '--title', $prTitle, '--body-file', $bodyFile))
        $out
        $pr = if ($DryRun) { '<new>' } else { ($out -join "`n") -replace '(?s).*?/pull/(\d+).*', '$1' }
        if (-not $DryRun -and $pr -notmatch '^\d+$') { throw "PR CREATE FAIL: $($out -join ' ')" }
    }
    "PR #$pr ($prBranch -> dev)"

    # --- Chờ check ------------------------------------------------------------------------------------------------------
    if (-not $ChecksTimeoutMin) { $ChecksTimeoutMin = if ($env:AG_PR_CHECKS_TIMEOUT_MIN) { [int]$env:AG_PR_CHECKS_TIMEOUT_MIN } else { 90 } }
    $required = @()
    $j = Invoke-AgRead gh @('api', "repos/$gh/branches/dev/protection/required_status_checks", '--jq', '[(.contexts // [])[], ((.checks // [])[] | .context)] | unique | .[]')
    if ($LASTEXITCODE -eq 0) { $required += @($j | Where-Object { $_ }) }
    $j = Invoke-AgRead gh @('api', "repos/$gh/rules/branches/dev", '--jq', '.[] | select(.type == "required_status_checks") | .parameters.required_status_checks[].context')
    if ($LASTEXITCODE -eq 0) { $required += @($j | Where-Object { $_ }) }
    $required = @($required | Sort-Object -Unique)
    if ($required.Count) { "required checks on dev: $($required -join ', ')" } else { 'no required checks on dev (branch protection not enabled yet): waiting for all checks reported on the head commit' }
    if ($DryRun) {
        if ($required.Count) { "[dry-run] poll: gh pr checks $pr -R $gh --required --json name,bucket (timeout ${ChecksTimeoutMin}m)" }
        else { "[dry-run] poll: gh pr checks $pr -R $gh --json name,bucket (timeout ${ChecksTimeoutMin}m; none after grace -> warn and continue)" }
    } else {
        $deadline = (Get-Date).AddMinutes($ChecksTimeoutMin)
        $grace = (Get-Date).AddSeconds($(if ($env:AG_PR_CHECKS_GRACE_SEC) { [int]$env:AG_PR_CHECKS_GRACE_SEC } else { 180 }))
        $result = $null
        while (-not $result) {
            $prHead = "$(gh pr view $pr -R $gh --json headRefOid -q .headRefOid 2>$null)".Trim()
            $checks = @()
            if ($prHead -eq $head) {
                $a = @('pr', 'checks', $pr, '-R', $gh, '--json', 'name,bucket,state'); if ($required.Count) { $a += '--required' }
                $raw = (& gh @a 2>$null | Out-String).Trim()
                if ($raw.StartsWith('[')) { $checks = @($raw | ConvertFrom-Json | ForEach-Object { $_ }) }
            }
            $fail = @($checks | Where-Object { $_.bucket -in @('fail', 'cancel') })
            $pending = @($checks | Where-Object { $_.bucket -eq 'pending' })
            if ($required.Count) {
                $missing = @($required | Where-Object { $_ -notin @($checks | ForEach-Object { $_.name }) })
                if ($fail.Count) { $result = "FAIL: $(($fail | ForEach-Object { $_.name }) -join '; ')" }
                elseif (-not $pending.Count -and -not $missing.Count -and $prHead -eq $head) { $result = 'OK' }
            } else {
                $hard = @($fail | Where-Object { -not $AllowFailingChecks -or $_.name -notmatch $AllowFailingChecks })
                if ($hard.Count) { $result = "FAIL: $(($hard | ForEach-Object { $_.name }) -join '; ')" }
                elseif ($prHead -eq $head -and $checks.Count -and -not $pending.Count) {
                    if ($fail.Count) { "WARNING: allowed failing checks: $(($fail | ForEach-Object { $_.name }) -join '; ')" }
                    $result = 'OK'
                } elseif ($prHead -eq $head -and -not $checks.Count -and (Get-Date) -gt $grace) {
                    'WARNING: no checks reported on the PR head; continuing without CI (enable CI on dev, ticket #132)'; $result = 'OK'
                }
            }
            if (-not $result -and (Get-Date) -gt $deadline) { $result = "TIMEOUT after ${ChecksTimeoutMin}m (pending: $(($pending | ForEach-Object { $_.name }) -join '; '))" }
            if (-not $result) { Start-Sleep -Seconds 30 }
        }
        "checks: $result ($($checks.Count) reported)"
        if ($result -ne 'OK') { "PR CHECKS $result - PR #$pr left open, worktree retained"; exit 5 }
    }

    # --- Gộp PR ---------------------------------------------------------------------------------------------------------
    Invoke-AgMut gh @('pr', 'merge', $pr, '-R', $gh, '--merge', '--match-head-commit', $head, '--delete-branch', '--subject', "${Ticket}: gộp PR #$pr vào dev (Refs #$Issue)")
    if ($LASTEXITCODE -ne 0) { "PR MERGE FAIL - PR #$pr left open"; exit 6 }
    if ($DryRun) {
        "[dry-run] verify: git fetch origin dev; git merge-base --is-ancestor $head origin/dev"
        if ($dryBlocked) { "DRY-RUN BLOCKED - review gate: a real merge exits 4 before the local gates (head $head)"; exit 4 }
        "DRY-RUN OK - would merge via PR (head $head)"
        exit 0
    }
    git -C $w fetch -q origin +refs/heads/dev:refs/remotes/origin/dev
    git -C $w log --oneline -1 origin/dev
    git -C $w merge-base --is-ancestor $head origin/dev
    if ($LASTEXITCODE -ne 0) { throw "Remote dev does not contain PR head $head - worktree retained" }
    "MERGED via PR #$pr head $head dev $("$(git -C $w rev-parse --short origin/dev)".Trim()) - worktree and branch retained for review"
    exit 0
} finally {
    if ($dryWt) {
        git -C $dryWt rebase --abort 2>&1 | Out-Null
        git -C $ctx.Repo worktree remove --force $dryWt 2>&1 | Out-Null
    }
}
