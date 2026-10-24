param(
    [Parameter(Mandatory)][Alias('Pkg')][string]$Worktree,   # thư mục dưới AG_WORKTREE_ROOT
    [Parameter(Mandatory)][string]$Ticket,                     # mã gói, vd P0-WEB-IDEMKEY -> nhánh PR pr/<Ticket>
    [Parameter(Mandatory)][int]$Issue,
    [Parameter(Mandatory)][ValidateSet('claude', 'codex')][string]$Agent,   # bên chạy gộp (chỉ để ghi log)
    [Parameter(Mandatory)][ValidateSet('claude', 'codex')][string]$Owner,   # chủ gói (nhãn agent:<x>); cần Review OK của bên kia
    [string]$IssueTitle = '',
    [string[]]$Tests = @(),
    [string]$GateRun = 'SchemaIsolation|RLSExplain|ModuleGroup|LoginRoles|NoCrossModule|LedgerAddColumn',
    [int]$ChecksTimeoutMin = 0,          # 0 -> AG_PR_CHECKS_TIMEOUT_MIN hoặc 90
    [string[]]$AllowFailingCheck = @(),  # TÊN CHÍNH XÁC check không bắt buộc được miễn khi dev CHƯA có check bắt buộc (ghi vào PR/issue)
    [switch]$NoChecksOk,                 # chỉ trước khi có CI: cho gộp khi PR không có check nào (ghi vào PR/issue)
    [switch]$DryRun
)
# Rebase nhánh gói lên origin/dev, gỡ xung đột tài liệu (giữ cả hai) và tệp sinh (lấy dev rồi sinh lại), kiểm cổng review
# chéo, chạy cổng + kiểm thử, rồi đẩy pr/<Ticket>, tạo/dùng lại PR vào dev, chờ check, gộp bằng merge commit đúng SHA đầu.
# Không còn đường push thẳng dev. Mã thoát: 0 gộp xong; 1 cổng/kiểm thử/regen/toolchain; 2 xung đột mã; 4 review; 5 check PR;
# 6 gộp PR. CI fail-closed: chỉ gộp khi mọi check (bắt buộc, hoặc mọi check báo về khi chưa bật bảo vệ nhánh) là 'pass'.
# -DryRun: rebase thử trong worktree tạm (không đụng nhánh gói), chạy thật cổng review (chỉ đọc), in mọi lệnh còn lại.
$ErrorActionPreference = 'Continue'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$sp = Split-Path -Parent $MyInvocation.MyCommand.Path
. (Join-Path $sp 'workflow-lib.ps1')
$script:AgDryRun = [bool]$DryRun
if (-not $DryRun -and -not $env:AG_INTEGRATION_LOCK) { throw 'Run through scripts/workflow/ticket.ps1 merge (holds origin lock/integration); direct runs are refused' }
$ctx = Get-AgContext $sp
$gh = $ctx.GhRepo
$AllowFailingCheck = @($AllowFailingCheck | Where-Object { "$_" -ne '' })
Assert-AgCheckWaivers $AllowFailingCheck
$waiverNote = @()
if ($AllowFailingCheck.Count) { $waiverNote += "WAIVER REQUESTED: -AllowFailingCheck $(($AllowFailingCheck | ForEach-Object { "'$_'" }) -join ', ') (exact names; only while dev has no required checks)" }
if ($NoChecksOk) { $waiverNote += 'WAIVER REQUESTED: -NoChecksOk (merge allowed without any CI check; only before CI exists on dev)' }
$waiverNote
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

# Toolchain Go chuẩn (AG_GO_TOOLCHAIN > GOTOOLCHAIN=goX.Y.Z > go.mod toolchain): đặt GOTOOLCHAIN đúng bản đó cho sinh lại và
# cổng cục bộ, in `go version`, dừng nếu Go đang chạy khác bản chuẩn (không để SDK máy mới hơn tạo golden/regression giả).
$goModText = if (Test-Path -LiteralPath (Join-Path $w 'go.mod')) { [IO.File]::ReadAllText((Join-Path $w 'go.mod')) } else { '' }
$tc = Resolve-AgGoToolchain $env:AG_GO_TOOLCHAIN $env:GOTOOLCHAIN $goModText
if (-not $tc) { 'TOOLCHAIN UNKNOWN: set AG_GO_TOOLCHAIN=goX.Y.Z (or GOTOOLCHAIN) to the CI toolchain'; if (-not $DryRun) { exit 1 } }
$goVersionLine = '(dry-run: not executed)'
if ($tc) {
    "go toolchain: $($tc.Version) (from $($tc.Source))"
    if ($DryRun) { "[dry-run] GOTOOLCHAIN=$($tc.Version); go version must report $($tc.Version)" }
    else {
        $env:GOTOOLCHAIN = $tc.Version
        $goVersionLine = "$(go version 2>&1)".Trim()
        "go version: $goVersionLine"
        if ($goVersionLine -notmatch ('^go version ' + [regex]::Escape($tc.Version) + ' ')) { "TOOLCHAIN MISMATCH: expected $($tc.Version)"; exit 1 }
    }
}

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
    $openPr = $null
    try { $openPr = Get-AgOpenPr $gh $prBranch } catch { "REVIEW READ ERROR: $_"; if (-not $DryRun) { exit 4 }; $dryBlocked = $true }
    "package owner: $Owner (review required from $(Get-AgOtherAgent $Owner)); merging agent: $Agent"
    $gate = Get-AgLiveReviewGate $gh $Issue $(if ($openPr) { "$($openPr.number)" } else { '' }) $Owner $head $w 'origin/dev' $Ticket
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
        $(if ($gate.Review -and $gate.Ok) { "Chủ gói: $Owner. ``$($gate.Review.Line)`` — $($gate.Review.Url) (khớp: $($gate.Mode))" } else { '(chưa có — dry-run)' }), '',
        '### Ngoại lệ CI', '', $(if ($waiverNote.Count) { $waiverNote -join "`n" } else { 'Không có (mọi check phải pass).' }), '',
        "Go: $goVersionLine", '',
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

    # --- Chờ check (fail-closed) -----------------------------------------------------------------------------------------
    if (-not $ChecksTimeoutMin) { $ChecksTimeoutMin = if ($env:AG_PR_CHECKS_TIMEOUT_MIN) { [int]$env:AG_PR_CHECKS_TIMEOUT_MIN } else { 90 } }
    $graceSec = if ($env:AG_PR_CHECKS_GRACE_SEC) { [int]$env:AG_PR_CHECKS_GRACE_SEC } else { 180 }
    $req = Get-AgRequiredChecks $gh 'dev'
    if (-not $req.Ok) { "PR CHECKS FAIL: cannot read required checks of dev ($($req.Error)) - PR #$pr left open"; if ($DryRun) { $dryBlocked = $true } else { exit 5 } }
    $required = @($req.Names)
    if ($required.Count) { "required checks on dev: $($required -join ', ')" } else { 'no required checks on dev (branch protection not enabled yet): every reported check must pass' }
    if ($required.Count -and $AllowFailingCheck.Count) { 'PR CHECKS FAIL: -AllowFailingCheck is refused while dev has required checks'; if ($DryRun) { $dryBlocked = $true } else { exit 5 } }
    if ($DryRun) {
        "[dry-run] poll: gh pr checks $pr -R $gh$(if ($required.Count) { ' --required' }) --json name,bucket,state (timeout ${ChecksTimeoutMin}m, grace ${graceSec}s; only bucket=pass is green; none after grace -> $(if ($NoChecksOk) { 'NO-CHECKS-OK (explicit)' } else { 'FAIL' }))"
    } else {
        $ck = Wait-AgPrChecks $gh $pr $head $required ($ChecksTimeoutMin * 60) $graceSec 30 ([bool]$NoChecksOk) $AllowFailingCheck
        "checks: $($ck.Result) - $($ck.Message) ($(@($ck.Checks).Count) reported)"
        if ($ck.Result -ne 'OK') { "PR CHECKS FAIL - PR #$pr left open, worktree retained"; exit 5 }
        $ciNote = @()
        if (@($ck.Waived).Count) { $ciNote += "WAIVED failing checks (explicit -AllowFailingCheck): $(@($ck.Waived) -join '; ')" }
        if ($ck.NoChecks) { $ciNote += 'NO-CHECKS-OK: merged without any CI check (explicit -NoChecksOk)' }
        if ($ciNote.Count) {
            $ciNote
            Invoke-AgMut gh @('pr', 'comment', $pr, '-R', $gh, '--body', ("Ngoại lệ CI khi gộp (head ``$head``):`n" + (($ciNote | ForEach-Object { "- $_" }) -join "`n")))
        }
    }

    # --- Cổng review lần 2, sát trước khi gộp: phán quyết có thể đã đổi trong lúc chờ cổng cục bộ/CI ----------------------
    $gate2 = Get-AgLiveReviewGate $gh $Issue $(if ($pr -match '^\d+$') { $pr } else { '' }) $Owner $head $w 'origin/dev' $Ticket
    "final review re-check: $($gate2.Message)"
    if (-not $gate2.Ok) {
        if ($DryRun) { $dryBlocked = $true } else { "REVIEW CHANGED before merge - PR #$pr left open"; exit 4 }
    }

    # --- Gộp PR ---------------------------------------------------------------------------------------------------------
    Invoke-AgMut gh @('pr', 'merge', $pr, '-R', $gh, '--merge', '--match-head-commit', $head, '--delete-branch', '--subject', "${Ticket}: gộp PR #$pr vào dev (Refs #$Issue)")
    if ($LASTEXITCODE -ne 0) { "PR MERGE FAIL - PR #$pr left open"; exit 6 }
    if ($DryRun) {
        "[dry-run] verify: git fetch origin dev; git merge-base --is-ancestor $head origin/dev"
        if ($dryBlocked) { "DRY-RUN BLOCKED - a real merge would stop (review gate exit 4 / checks exit 5; see above) (head $head)"; exit 4 }
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
