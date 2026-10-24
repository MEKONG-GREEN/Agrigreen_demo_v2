# Tự kiểm các hàm thuần của workflow-lib.ps1 (không cần Pester, không gọi GitHub).
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
Check 'parse variants (hyphen, bold, backticks, upper-case sha, mojibake; not mid-line/quoted/short/other tool)' (($v | ForEach-Object { "$($_.Verdict):$($_.Reviewer):$($_.Sha)" }) -join ' ') 'OK:claude:deadbeef12 CHANGES:codex:1111111 OK:codex:2222222 OK:codex:5555555'
$v = @(Get-AgReviewVerdicts @((C "Review OK $D Codex (review chéo tại aaaaaaa)" '2026-10-24T09:00:00Z'), (C "Review CHANGES $D Codex (review chéo tại bbbbbbb)" '2026-10-24T08:00:00Z')))
Check 'sorted by createdAt' (($v | ForEach-Object { $_.Sha }) -join ' ') 'bbbbbbb aaaaaaa'

# --- So SHA -------------------------------------------------------------------------------------------------------------
$full = '0123456789abcdef0123456789abcdef01234567'
Check 'sha short 7' (Test-AgShaMatch '0123456' $full) 'True'
Check 'sha full' (Test-AgShaMatch $full $full) 'True'
Check 'sha upper-case' (Test-AgShaMatch '0123456789ABCDEF' $full) 'True'
Check 'sha 6 chars rejected' (Test-AgShaMatch '012345' $full) 'False'
Check 'sha not prefix' (Test-AgShaMatch '1234567' $full) 'False'
Check 'sha non-hex' (Test-AgShaMatch '012345g' $full) 'False'
Check 'sha empty' (Test-AgShaMatch '' $full) 'False'

# --- Bên review ---------------------------------------------------------------------------------------------------------
Check 'other of claude' (Get-AgOtherAgent 'claude') 'codex'
Check 'other of Codex' (Get-AgOtherAgent 'Codex') 'claude'
Check 'generated path' ((Test-AgGeneratedPath 'internal/x/modreg_gen.go'), (Test-AgGeneratedPath 'apps/web-ops/src/api/generated/schema.ts'), (Test-AgGeneratedPath 'internal/x/a.go')) 'True True False'

# --- Repo git tạm: quyết định "rebase chỉ đổi tệp sinh" -----------------------------------------------------------------
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
    Put 'a.go' @(1..30 | ForEach-Object { "a$_" }); Put 'b.go' @('b1'); Put 'contracts/openapi/application.yaml' @('v: 1'); Put 'docs/x.md' @('# x')
    G add -A; G commit -q -m base
    G checkout -q -b pkg
    Put 'a.go' (@('A1') + @(2..30 | ForEach-Object { "a$_" })); Put 'contracts/openapi/application.yaml' @('v: 1', 'pkg: 1')
    G commit -q -am 'pkg change'
    $pkgOld = Rev HEAD
    G checkout -q dev
    Put 'b.go' @('b1', 'b2'); Put 'a.go' (@(1..29 | ForEach-Object { "a$_" }) + @('A30')); Put 'contracts/openapi/application.yaml' @('v: 2')
    G commit -q -am 'dev moves (other file, far line of a.go, generated file)'
    G checkout -q pkg
    G rebase -q dev
    if ("$(git -C $tmp diff --name-only --diff-filter=U 2>$null)") {   # tệp sinh xung đột: lấy dev, như rebase-merge.ps1
        G checkout --ours -- contracts/openapi/application.yaml; G add -A; $env:GIT_EDITOR = 'true'; G rebase --continue
    }
    Put 'contracts/openapi/application.yaml' @('v: 2', 'pkg: 1'); G commit -q -am 'regenerate'
    $head = Rev HEAD
    Check 'temp repo rebased' ((Rev "$head~2") -eq (Rev dev)) 'True'
    $r = Test-AgReviewCovers $tmp $pkgOld.Substring(0, 8) $head 'dev'
    Check 'pre-rebase review covers rebased head (only generated + base changed)' "$($r.Covered) $($r.Mode)" 'True equivalent'
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

    # Gate end-to-end on the temp repo.
    $cm = @(
        (C (Format-AgReviewLine OK codex $pkgOld.Substring(0, 9)) '2026-10-24T01:00:00Z' 'c1'),
        (C (Format-AgReviewLine OK claude $head2) '2026-10-24T02:00:00Z' 'c2'))
    $g = Resolve-AgReviewGate @(Get-AgReviewVerdicts $cm) 'claude' $head 'C:\nonexistent' 'dev' 'PKG'
    Check 'gate: review sha unresolvable (wrong repo dir) -> not ok' "$($g.Ok)" 'False'
    $g = Resolve-AgReviewGate @(Get-AgReviewVerdicts $cm) 'claude' $head $tmp 'dev' 'PKG'
    Check 'gate: claude merging, Codex OK at pre-rebase sha -> ok (equivalent)' "$($g.Ok) $($g.Mode) $($g.Review.Url)" 'True equivalent c1'
    $g = Resolve-AgReviewGate @(Get-AgReviewVerdicts $cm) 'codex' $head2 $tmp 'dev' 'PKG'
    Check 'gate: codex merging needs Claude review; Claude OK at head2 -> exact' "$($g.Ok) $($g.Mode) $($g.Review.Url)" 'True exact c2'
    $g = Resolve-AgReviewGate @(Get-AgReviewVerdicts $cm) 'claude' $head2 $tmp 'dev' 'PKG'
    Check 'gate: stale review after non-generated change -> stop, ask re-review' "$($g.Ok) $($g.Message.StartsWith('REVIEW STALE'))" 'False True'
    $g = Resolve-AgReviewGate @(Get-AgReviewVerdicts @((C (Format-AgReviewLine OK claude $head) '2026-10-24T01:00:00Z'))) 'claude' $head $tmp 'dev' 'PKG'
    Check 'gate: self-review ignored' "$($g.Ok) $($g.Message.StartsWith('REVIEW MISSING'))" 'False True'
    $cm2 = $cm + @((C (Format-AgReviewLine CHANGES codex $head) '2026-10-24T03:00:00Z'))
    $g = Resolve-AgReviewGate @(Get-AgReviewVerdicts $cm2) 'claude' $head $tmp 'dev' 'PKG'
    Check 'gate: later CHANGES invalidates earlier OK' "$($g.Ok) $($g.Message.StartsWith('REVIEW CHANGES'))" 'False True'
    $cm3 = $cm2 + @((C (Format-AgReviewLine OK codex $head.Substring(0, 7)) '2026-10-24T04:00:00Z'))
    $g = Resolve-AgReviewGate @(Get-AgReviewVerdicts $cm3) 'claude' $head $tmp 'dev' 'PKG'
    Check 'gate: OK after CHANGES -> ok' "$($g.Ok) $($g.Mode)" 'True exact'
} finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
"selftest: $script:pass passed, $script:fail failed"
if ($script:fail) { exit 1 } else { exit 0 }
