# Tự kiểm workflow-lib.ps1 (không cần Pester, không gọi GitHub: `gh` được thay bằng hàm giả trong tệp này).
# Chạy: powershell -NoProfile -ExecutionPolicy Bypass -File scripts\workflow\selftest.ps1   -> mã thoát 0 khi mọi ca qua.
$ErrorActionPreference = 'Continue'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
. (Join-Path (Split-Path -Parent $MyInvocation.MyCommand.Path) 'workflow-lib.ps1')
$script:pass = 0; $script:fail = 0
function Check([string]$name, $actual, $expected) {
    if ("$actual" -ceq "$expected") { $script:pass++; "ok   $name" } else { $script:fail++; "FAIL $name`n     got:      $actual`n     expected: $expected" }
}
$D = [char]0x2014
function C([string]$body, [string]$at, [string]$url = 'u') { [pscustomobject]@{ body = $body; createdAt = $at; url = $url } }
function VS($v) { ($v | ForEach-Object { "$($_.Verdict):$($_.Reviewer):$($_.Sha)" }) -join ' ' }

# --- Phân tích bình luận ----------------------------------------------------------------------------------------------
$line = Format-AgReviewLine OK codex 'abc1234'
Check 'format line' $line "Review OK $D Codex (review ch$([char]0xE9)o t$([char]0x1EA1)i abc1234)"
$v = @(Get-AgReviewVerdicts @((C $line '2026-10-24T01:00:00Z')))
Check 'parse own format' "$($v.Count) $($v[0].Verdict) $($v[0].Reviewer) $($v[0].Sha)" '1 OK codex abc1234'
$v = @(Get-AgReviewVerdicts @(
    (C "**Review OK - Claude (review chéo tại ``DEADBEEF12``)**`nghi chú" '2026-10-24T02:00:00Z'),
    (C "Review CHANGES $D Codex (review chéo tại 1111111)`nReview OK -- Codex (review chéo tại 2222222)" '2026-10-24T03:00:00Z'),
    (C "Đang chờ Review OK $D Codex (review chéo tại 3333333)" '2026-10-24T04:00:00Z'),
    (C "> Review OK $D Codex (review chéo tại 4444444)" '2026-10-24T05:00:00Z'),
    (C "Review OK $D Codex (review chÃ©o táº¡i 5555555)" '2026-10-24T06:00:00Z'),
    (C "Review OK $D Codex (review chéo tại 66666)" '2026-10-24T07:00:00Z'),
    (C "Review OK $D Gemini (review chéo tại 7777777)" '2026-10-24T08:00:00Z')))
Check 'parse variants (hyphen, bold, backticks, upper-case sha, mojibake; not mid-line/quoted/short/other tool)' (VS $v) 'OK:claude:deadbeef12 CHANGES:codex:1111111 OK:codex:2222222 OK:codex:5555555'
$v = @(Get-AgReviewVerdicts @((C "Review OK $D Codex (review chéo tại aaaaaaa)" '2026-10-24T09:00:00Z'), (C "Review CHANGES $D Codex (review chéo tại bbbbbbb)" '2026-10-24T08:00:00Z')))
Check 'sorted by createdAt' (($v | ForEach-Object { $_.Sha }) -join ' ') 'bbbbbbb aaaaaaa'
# #142 review điểm 5: ví dụ trong khối code / chú thích / thụt 4 dấu cách không phải phán quyết.
$ex = "Mẫu cần đăng:`n``````text`nReview OK $D Codex (review chéo tại 8888888)`n```````n~~~~`nReview OK $D Codex (review chéo tại 8888889)`n~~~~`n<!-- Review OK $D Codex (review chéo tại 888888a) -->`n    Review OK $D Codex (review chéo tại 888888b)`n`tReview OK $D Codex (review chéo tại 888888c)"
Check 'example inside code fence/tilde fence/HTML comment/indented code is not a verdict' (@(Get-AgReviewVerdicts @((C $ex '2026-10-24T01:00:00Z'))).Count) '0'
Check 'unclosed fence hides the rest of the comment' (@(Get-AgReviewVerdicts @((C "``````n`nReview OK $D Codex (review chéo tại 9999999)" '2026-10-24T01:00:00Z'))).Count) '0'
Check 'verdict after a closed fence still counts' (VS @(Get-AgReviewVerdicts @((C "``````n x`n```````nReview OK $D Codex (review chéo tại 9999999)" '2026-10-24T01:00:00Z')))) 'OK:codex:9999999'
# Mẫu yêu cầu sửa cũ (CODEX-MERGE 0.3) -> CHANGES; không bao giờ là OK.
$v = @(Get-AgReviewVerdicts @(
    (C "Codex review $D cần sửa trước Review OK (abcdef1)." '2026-10-24T01:00:00Z'),
    (C "Codex review snapshot **cd983f2f6d** trên nhánh riêng. Có 3 điểm P2 cần sửa trước duyệt:" '2026-10-24T02:00:00Z'),
    (C "Claude review: changes requested (1234abc)" '2026-10-24T03:00:00Z'),
    (C "Codex review $D OK (abcdef2)" '2026-10-24T04:00:00Z'),
    (C "Ghi chú: Codex review $D cần sửa trước Review OK (abcdef3) giữa dòng" '2026-10-24T05:00:00Z')))
Check 'legacy change requests -> CHANGES; legacy OK / mid-line ignored' (VS $v) 'CHANGES:codex:abcdef1 CHANGES:codex:cd983f2f6d CHANGES:claude:1234abc'
Check 'legacy verdict flagged' "$($v[0].Legacy)" 'True'

# --- So SHA -------------------------------------------------------------------------------------------------------------
$full = '0123456789abcdef0123456789abcdef01234567'
Check 'sha short 7' (Test-AgShaMatch '0123456' $full) 'True'
Check 'sha full' (Test-AgShaMatch $full $full) 'True'
Check 'sha upper-case' (Test-AgShaMatch '0123456789ABCDEF' $full) 'True'
Check 'sha 6 chars rejected' (Test-AgShaMatch '012345' $full) 'False'
Check 'sha not prefix' (Test-AgShaMatch '1234567' $full) 'False'
Check 'sha non-hex' (Test-AgShaMatch '012345g' $full) 'False'
Check 'sha empty' (Test-AgShaMatch '' $full) 'False'

# --- Bên review, chủ gói, tệp sinh --------------------------------------------------------------------------------------
Check 'other of claude' (Get-AgOtherAgent 'claude') 'codex'
Check 'other of Codex' (Get-AgOtherAgent 'Codex') 'claude'
Check 'generated path' ((Test-AgGeneratedPath 'internal/x/modreg_gen.go'), (Test-AgGeneratedPath 'apps/web-ops/src/api/generated/schema.ts'), (Test-AgGeneratedPath 'internal/x/a.go')) 'True True False'
Check 'hand-edited menu sources are NOT generated (#142 review 2)' ((Test-AgGeneratedPath 'internal/platform/party/domain/menu_departments.go'), (Test-AgGeneratedPath 'internal/platform/party/domain/kn_test.go')) 'False False'
Check 'owner from label' (Resolve-AgOwner @('st:review', 'agent:claude', 'lane:kn')) 'claude'
$e = try { Resolve-AgOwner @('st:review') } catch { 'threw' }; Check 'owner missing -> error' $e 'threw'
$e = try { Resolve-AgOwner @('agent:claude', 'agent:codex') } catch { 'threw' }; Check 'two owners -> error' $e 'threw'

# --- Toolchain Go -------------------------------------------------------------------------------------------------------
$gm = "module x`n`ngo 1.26`n`ntoolchain go1.26.9`n"
Check 'toolchain: AG_GO_TOOLCHAIN wins' "$((Resolve-AgGoToolchain 'go1.26.10' 'go1.27.1' $gm).Version)" 'go1.26.10'
Check 'toolchain: explicit GOTOOLCHAIN next' "$((Resolve-AgGoToolchain '' 'go1.26.10' $gm).Version)" 'go1.26.10'
Check 'toolchain: auto ignored -> go.mod' "$((Resolve-AgGoToolchain '' 'auto' $gm).Version)" 'go1.26.9'
Check 'toolchain: none -> null' "$($null -eq (Resolve-AgGoToolchain '' 'local' "module x`ngo 1.26`n"))" 'True'

# --- Repo git tạm: quyết định "review cũ còn phủ đầu PR" ----------------------------------------------------------------
$tmp = Join-Path $env:TEMP ("agwf-selftest-" + [IO.Path]::GetRandomFileName().Replace('.', ''))
New-Item -ItemType Directory -Path $tmp | Out-Null
$env:GIT_AUTHOR_NAME = 't'; $env:GIT_AUTHOR_EMAIL = 't@example.invalid'; $env:GIT_COMMITTER_NAME = 't'; $env:GIT_COMMITTER_EMAIL = 't@example.invalid'
function G { git -C $tmp -c core.hooksPath=NUL -c commit.gpgsign=false -c core.autocrlf=false @args 2>&1 | Out-Null }
function Put([string]$path, [string[]]$lines) {
    $p = Join-Path $tmp $path; New-Item -ItemType Directory -Force -Path (Split-Path -Parent $p) | Out-Null
    [IO.File]::WriteAllText($p, (($lines -join "`n") + "`n"))
}
function Rev([string]$r) { "$(git -C $tmp rev-parse $r)".Trim() }
try {
    G init -q -b dev
    Put 'a.go' @(1..30 | ForEach-Object { "a$_" }); Put 'b.go' @('b1'); Put 'c.go' @('c1'); Put 'contracts/openapi/application.yaml' @('v: 1'); Put 'docs/x.md' @('# x')
    G add -A; G commit -q -m base
    G checkout -q -b pkg
    Put 'a.go' (@('A1') + @(2..30 | ForEach-Object { "a$_" })); Put 'contracts/openapi/application.yaml' @('v: 1', 'pkg: 1')
    G commit -q -am 'pkg change'
    $pkgOld = Rev HEAD
    G checkout -q dev
    Put 'b.go' @('b1', 'b2'); Put 'contracts/openapi/application.yaml' @('v: 2')
    G commit -q -am 'dev moves (file the package does not touch, generated file)'
    G checkout -q pkg
    G rebase -q dev
    if ("$(git -C $tmp diff --name-only --diff-filter=U 2>$null)") {   # tệp sinh xung đột: lấy dev, như rebase-merge.ps1
        G checkout --ours -- contracts/openapi/application.yaml; G add -A; $env:GIT_EDITOR = 'true'; G rebase --continue
    }
    Put 'contracts/openapi/application.yaml' @('v: 2', 'pkg: 1'); G commit -q -am 'regenerate'
    $head = Rev HEAD
    Check 'temp repo rebased' ((Rev "$head~2") -eq (Rev dev)) 'True'
    $r = Test-AgReviewCovers $tmp $pkgOld.Substring(0, 8) $head 'dev'
    Check 'pre-rebase review covers rebased head (dev changed only other + generated files)' "$($r.Covered) $($r.Mode)" 'True equivalent'
    $r = Test-AgReviewCovers $tmp $head.Substring(0, 7) $head 'dev'
    Check 'review at head (short sha) = exact' "$($r.Covered) $($r.Mode)" 'True exact'
    $r = Test-AgReviewCovers $tmp 'fedcba9876' $head 'dev'
    Check 'unknown sha not covered' "$($r.Covered)" 'False'

    Put 'b.go' @('b1', 'b2', 'pkg-extra'); G commit -q -am 'extra non-generated change after review'
    $head2 = Rev HEAD
    $r = Test-AgReviewCovers $tmp $pkgOld $head2 'dev'
    Check 'non-generated change after review -> not covered' "$($r.Covered)" 'False'
    $r = Test-AgReviewCovers $tmp $head $head2 'dev'
    Check 'review at previous rebased head -> not covered' "$($r.Covered)" 'False'

    # Dev cũng sửa tệp của gói (dòng xa, không xung đột) -> phải review lại dù rebase sạch.
    G checkout -q -b pkg3 $pkgOld
    G checkout -q -b dev3 dev
    Put 'a.go' (@(1..29 | ForEach-Object { "a$_" }) + @('A30')); G commit -q -am 'dev edits a far line of a package file'
    G checkout -q pkg3; G rebase -q dev3
    $head3 = Rev HEAD
    $r = Test-AgReviewCovers $tmp $pkgOld $head3 'dev3'
    Check 'dev also touched a package file -> re-review required' "$($r.Covered) $($r.Reason -match 'dev also changed package file')" 'False True'

    # #142 review điểm 2, repro của reviewer: cùng dòng +/- nhưng ở hàm khác -> KHÔNG tương đương.
    G checkout -q -b dev4 dev
    Put 'p.go' @('package p', '', 'func allowLowRisk() bool {', '    return false', '}', '', 'func allowPrivileged() bool {', '    return false', '}')
    G add -A; G commit -q -m 'policy base'
    G checkout -q -b pkg4
    Put 'p.go' @('package p', '', 'func allowLowRisk() bool {', '    return true', '}', '', 'func allowPrivileged() bool {', '    return false', '}')
    G commit -q -am 'reviewed: allowLowRisk true'
    $rev4 = Rev HEAD
    Put 'p.go' @('package p', '', 'func allowLowRisk() bool {', '    return false', '}', '', 'func allowPrivileged() bool {', '    return true', '}')
    G commit -q -am 'after review: revert allowLowRisk, allowPrivileged true'
    $head4 = Rev HEAD
    $r = Test-AgReviewCovers $tmp $rev4 $head4 'dev4'
    Check 'reviewer repro: same +/- lines in a different function -> not covered' "$($r.Covered) $($r.Mode)" 'False none'

    # Cổng review theo CHỦ GÓI, không theo bên gộp (#142 review điểm 4).
    $cm = @(
        (C (Format-AgReviewLine OK codex $pkgOld.Substring(0, 9)) '2026-10-24T01:00:00Z' 'c1'),
        (C (Format-AgReviewLine OK claude $head2) '2026-10-24T02:00:00Z' 'c2'))
    $g = Resolve-AgReviewGate @(Get-AgReviewVerdicts $cm) 'claude' $head 'C:\nonexistent' 'dev' 'PKG'
    Check 'gate: review sha unresolvable (wrong repo dir) -> not ok' "$($g.Ok)" 'False'
    $g = Resolve-AgReviewGate @(Get-AgReviewVerdicts $cm) 'claude' $head $tmp 'dev' 'PKG'
    Check 'gate: owner claude, Codex OK at pre-rebase sha -> ok (equivalent)' "$($g.Ok) $($g.Mode) $($g.Review.Url)" 'True equivalent c1'
    $g = Resolve-AgReviewGate @(Get-AgReviewVerdicts $cm) 'codex' $head2 $tmp 'dev' 'PKG'
    Check 'gate: owner codex needs Claude review; Claude OK at head2 -> exact' "$($g.Ok) $($g.Mode) $($g.Review.Url)" 'True exact c2'
    $g = Resolve-AgReviewGate @(Get-AgReviewVerdicts $cm) 'claude' $head2 $tmp 'dev' 'PKG'
    Check 'gate: stale review after non-generated change -> stop, ask re-review' "$($g.Ok) $($g.Message.StartsWith('REVIEW STALE'))" 'False True'
    $g = Resolve-AgReviewGate @(Get-AgReviewVerdicts @((C (Format-AgReviewLine OK claude $head) '2026-10-24T01:00:00Z'))) 'claude' $head $tmp 'dev' 'PKG'
    Check 'gate: owner self-review (Claude OK on Claude package) ignored, whoever merges' "$($g.Ok) $($g.Message.StartsWith('REVIEW MISSING'))" 'False True'
    $cm2 = $cm + @((C (Format-AgReviewLine CHANGES codex $head) '2026-10-24T03:00:00Z'))
    $g = Resolve-AgReviewGate @(Get-AgReviewVerdicts $cm2) 'claude' $head $tmp 'dev' 'PKG'
    Check 'gate: later CHANGES invalidates earlier OK' "$($g.Ok) $($g.Message.StartsWith('REVIEW CHANGES'))" 'False True'
    $cm3 = $cm2 + @((C (Format-AgReviewLine OK codex $head.Substring(0, 7)) '2026-10-24T04:00:00Z'))
    $g = Resolve-AgReviewGate @(Get-AgReviewVerdicts $cm3) 'claude' $head $tmp 'dev' 'PKG'
    Check 'gate: OK after CHANGES -> ok' "$($g.Ok) $($g.Mode)" 'True exact'
    $cm4 = $cm3 + @((C "Codex review $D cần sửa trước Review OK ($($head.Substring(0, 8)))." '2026-10-24T05:00:00Z'))
    $g = Resolve-AgReviewGate @(Get-AgReviewVerdicts $cm4) 'claude' $head $tmp 'dev' 'PKG'
    Check 'gate: legacy "cần sửa" after OK revokes it (#142 review 5)' "$($g.Ok) $($g.Message.StartsWith('REVIEW CHANGES'))" 'False True'
    $cm5 = @((C "Mẫu:`n``````n$(Format-AgReviewLine OK codex $head)`n``````" '2026-10-24T01:00:00Z'))
    $g = Resolve-AgReviewGate @(Get-AgReviewVerdicts $cm5) 'claude' $head $tmp 'dev' 'PKG'
    Check 'gate: OK only inside a code block -> missing' "$($g.Ok) $($g.Message.StartsWith('REVIEW MISSING'))" 'False True'

    # --- `gh` giả: cổng review đọc lại sát trước khi gộp (#142 review điểm 3) --------------------------------------------
    $script:ghImpl = $null; $script:ghCalls = @()
    function global:gh { $script:ghCalls += ($args -join ' '); & $script:ghImpl @args }
    $okBody = Format-AgReviewLine OK codex $head
    $script:issueComments = @(@{ body = $okBody; createdAt = '2026-10-24T01:00:00Z'; url = 'ok1' })
    $script:ghImpl = {
        if ($args[0] -eq 'issue' -and $args[1] -eq 'view') { $global:LASTEXITCODE = 0; return (@{ comments = $script:issueComments } | ConvertTo-Json -Depth 5) }
        if ($args[0] -eq 'pr' -and $args[1] -eq 'view') { $global:LASTEXITCODE = 0; return '{"comments":[],"reviews":[]}' }
        $global:LASTEXITCODE = 1; Write-Error 'unexpected'
    }
    $g1 = Get-AgLiveReviewGate 'o/r' 142 '7' 'claude' $head $tmp 'dev' 'PKG'
    Check 'live gate before CI: Codex OK -> ok' "$($g1.Ok) $($g1.Mode)" 'True exact'
    $script:issueComments += @{ body = (Format-AgReviewLine CHANGES codex $head); createdAt = '2026-10-24T02:00:00Z'; url = 'ch1' }
    $g2 = Get-AgLiveReviewGate 'o/r' 142 '7' 'claude' $head $tmp 'dev' 'PKG'
    Check 'live gate re-read before merge: CHANGES posted while CI ran -> blocked' "$($g2.Ok) $($g2.Message.StartsWith('REVIEW CHANGES'))" 'False True'
    $script:ghImpl = { if ($args[0] -eq 'issue') { $global:LASTEXITCODE = 0; return (@{ comments = @(@{ body = $okBody; createdAt = '2026-10-24T01:00:00Z'; url = 'ok1' }) } | ConvertTo-Json -Depth 5) }; $global:LASTEXITCODE = 1; Write-Error 'HTTP 502: Bad Gateway' }
    $g3 = Get-AgLiveReviewGate 'o/r' 142 '7' 'claude' $head $tmp 'dev' 'PKG'
    Check 'live gate: PR comments unreadable -> blocked (not "no comments")' "$($g3.Ok) $($g3.Message.StartsWith('REVIEW READ ERROR'))" 'False True'
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}

# --- Check CI fail-closed (#142 review điểm 1), `gh` giả --------------------------------------------------------------
$H = '0123456789abcdef0123456789abcdef01234567'
function Ck([string]$name, [string]$bucket) { [pscustomobject]@{ name = $name; bucket = $bucket; state = $bucket.ToUpper() } }
# Kịch bản: $script:prHead, $script:checkSeq (mảng các phản hồi theo lượt; phần tử: mảng check | 'NOCHECKS' | 'ERROR').
function Set-GhChecks([object[]]$Seq, [string]$PrHead = $H) {
    $script:checkSeq = $Seq; $script:checkIdx = 0; $script:prHead = $PrHead
    $script:ghImpl = {
        if ($args[0] -eq 'pr' -and $args[1] -eq 'view') { $global:LASTEXITCODE = 0; return $script:prHead }
        if ($args[0] -eq 'pr' -and $args[1] -eq 'checks') {
            $i = [Math]::Min($script:checkIdx, $script:checkSeq.Count - 1); $script:checkIdx++
            $x = $script:checkSeq[$i]
            if ("$x" -eq 'NOCHECKS') { $global:LASTEXITCODE = 1; Write-Error "no checks reported on the 'pr/X' branch"; return }
            if ("$x" -eq 'ERROR') { $global:LASTEXITCODE = 1; Write-Error 'HTTP 502: Bad Gateway (https://api.github.com/graphql)'; return }
            $arr = @($x)
            $global:LASTEXITCODE = if (@($arr | Where-Object { $_.bucket -eq 'pending' }).Count) { 8 } elseif (@($arr | Where-Object { $_.bucket -ne 'pass' }).Count) { 1 } else { 0 }
            return (ConvertTo-Json -InputObject @($arr) -Depth 3)
        }
        $global:LASTEXITCODE = 1; Write-Error "unexpected gh $($args -join ' ')"
    }
}
function W([string[]]$Req = @(), [bool]$NoChecksOk = $false, [string[]]$Allow = @(), [int]$Timeout = 5, [int]$Grace = 0) {
    $x = Wait-AgPrChecks 'o/r' '7' $H $Req $Timeout $Grace 0 $NoChecksOk $Allow
    return "$($x.Result)|$($x.Message)"
}
function St([string]$s) { ($s -split '\|')[0] }

Set-GhChecks @(, @((Ck 'build' 'pass'), (Ck 'test' 'skipping')))
Check 'ci: required check SKIPPED -> FAIL' (St (W @('build', 'test'))) 'FAIL'
Set-GhChecks @(, @((Ck 'build' 'pass'), (Ck 'test' '')))
Check 'ci: required check UNKNOWN (empty bucket) -> FAIL' (St (W @('build', 'test'))) 'FAIL'
Set-GhChecks @(, @((Ck 'build' 'pass'), (Ck 'test' 'neutral')))
Check 'ci: required check with unexpected bucket -> FAIL' (St (W @('build', 'test'))) 'FAIL'
Set-GhChecks @(, @((Ck 'build' 'pass'), (Ck 'test' 'fail')))
Check 'ci: required check FAILURE -> FAIL' (St (W @('build', 'test'))) 'FAIL'
Set-GhChecks @(, @((Ck 'build' 'pass'), (Ck 'test' 'cancel')))
Check 'ci: required check cancelled -> FAIL' (St (W @('build', 'test'))) 'FAIL'
Set-GhChecks @(, @((Ck 'build' 'pass')))
$x = W @('build', 'test') -Timeout 0
Check 'ci: required check missing until timeout -> FAIL (TIMEOUT)' "$(St $x) $($x -match 'TIMEOUT')" 'FAIL True'
Set-GhChecks @(@((Ck 'build' 'pending'), (Ck 'test' 'pending')), @((Ck 'build' 'pass'), (Ck 'test' 'pass')))
Check 'ci: required pending then all pass -> OK' (St (W @('build', 'test'))) 'OK'
Set-GhChecks @(, @((Ck 'build' 'pass'), (Ck 'build' 'fail'), (Ck 'test' 'pass')))
Check 'ci: one of two runs of a required check failed -> FAIL' (St (W @('build', 'test'))) 'FAIL'
Set-GhChecks @(, @((Ck 'build' 'pass'), (Ck 'test' 'pass')))
Check 'ci: required + -AllowFailingCheck -> FAIL (waivers refused with branch protection)' (St (W @('build', 'test') -Allow @('lint'))) 'FAIL'
Set-GhChecks @('NOCHECKS')
$x = W
Check 'ci: no checks at all after grace -> FAIL (refuse)' "$(St $x) $($x -match 'refusing to merge without CI')" 'FAIL True'
Set-GhChecks @('NOCHECKS')
$y = Wait-AgPrChecks 'o/r' '7' $H @() 5 0 0 $true @()
Check 'ci: no checks + explicit -NoChecksOk -> OK, flagged NoChecks for the PR/issue record' "$($y.Result) $($y.NoChecks) $($y.Message.StartsWith('NO-CHECKS-OK'))" 'OK True True'
Set-GhChecks @('ERROR')
$x = W -NoChecksOk $true -Timeout 0
Check 'ci: API read error is not "no checks" (even with -NoChecksOk) -> FAIL' "$(St $x) $($x -match 'HTTP 502')" 'FAIL True'
Set-GhChecks @('ERROR', @((Ck 'build' 'pass')))
Check 'ci: transient read error then pass -> OK' (St (W)) 'OK'
Set-GhChecks @(, @((Ck 'build' 'pass'), (Ck 'lint' 'skipping')))
Check 'ci: no required checks, a reported check SKIPPED -> FAIL' (St (W)) 'FAIL'
Set-GhChecks @(, @((Ck 'build' 'pass'), (Ck 'lint' 'fail')))
Check 'ci: no required checks, a reported check failed -> FAIL' (St (W)) 'FAIL'
Set-GhChecks @(, @((Ck 'build' 'pass'), (Ck 'lint' 'fail')))
$y = Wait-AgPrChecks 'o/r' '7' $H @() 5 0 0 $false @('lint')
Check 'ci: exact-name waiver for a non-required failing check -> OK, waiver recorded' "$($y.Result) $(@($y.Waived) -join ',') $($y.Message -match 'WAIVED')" 'OK lint (fail) True'
Set-GhChecks @(, @((Ck 'build' 'fail'), (Ck 'lint' 'fail')))
Check 'ci: waiver of another name does not cover a failing check -> FAIL' (St (W -Allow @('lint'))) 'FAIL'
Set-GhChecks @(, @((Ck 'lint' 'fail')))
Check 'ci: every reported check waived (none passing) -> FAIL' (St (W -Allow @('lint'))) 'FAIL'
$e = try { Assert-AgCheckWaivers @('.*'); 'accepted' } catch { 'refused' }
Check 'ci: -AllowFailingCheck ".*" refused (no regex/wildcards)' $e 'refused'
$e = try { Assert-AgCheckWaivers @('lint*'); 'accepted' } catch { 'refused' }
Check 'ci: -AllowFailingCheck "lint*" refused' $e 'refused'
Set-GhChecks @(, @((Ck 'build' 'pass'))) -PrHead 'fedcba9876543210fedcba9876543210fedcba98'
$x = W
Check 'ci: PR head moved -> FAIL immediately' "$(St $x) $($x -match 'head moved')" 'FAIL True'
Set-GhChecks @(, @((Ck 'build' 'pass'), (Ck 'test' 'pass')))
Check 'ci: no required checks, all reported pass -> OK' (St (W)) 'OK'

# Đọc check bắt buộc: chỉ "chưa bảo vệ" mới là "không có"; lỗi khác -> Ok=False (người gọi dừng mã 5).
$script:ghImpl = {
    if ("$($args[1])" -like '*protection/required_status_checks') { $global:LASTEXITCODE = 1; Write-Error 'gh: Branch not protected (HTTP 404)'; return }
    $global:LASTEXITCODE = 0; return '[]'
}
$q = Get-AgRequiredChecks 'o/r'; Check 'required: branch not protected + no rules -> none' "$($q.Ok) $(@($q.Names).Count)" 'True 0'
$script:ghImpl = {
    if ("$($args[1])" -like '*protection/required_status_checks') { $global:LASTEXITCODE = 0; return '{"contexts":["build"],"checks":[{"context":"build"},{"context":"test"}]}' }
    $global:LASTEXITCODE = 0; return '[{"type":"required_status_checks","parameters":{"required_status_checks":[{"context":"sec"}]}}]'
}
$q = Get-AgRequiredChecks 'o/r'; Check 'required: protection + ruleset merged' "$($q.Ok) $(@($q.Names) -join ',')" 'True build,sec,test'
$script:ghImpl = { $global:LASTEXITCODE = 1; Write-Error 'HTTP 403: Resource not accessible' }
$q = Get-AgRequiredChecks 'o/r'; Check 'required: protection read error -> Ok=False (fail-closed)' "$($q.Ok)" 'False'
$script:ghImpl = {
    if ("$($args[1])" -like '*protection/required_status_checks') { $global:LASTEXITCODE = 1; Write-Error 'gh: Branch not protected (HTTP 404)'; return }
    $global:LASTEXITCODE = 1; Write-Error 'HTTP 500'
}
$q = Get-AgRequiredChecks 'o/r'; Check 'required: rules read error -> Ok=False (fail-closed)' "$($q.Ok)" 'False'
Remove-Item Function:\gh -ErrorAction SilentlyContinue

"selftest: $script:pass passed, $script:fail failed"
if ($script:fail) { exit 1 } else { exit 0 }
