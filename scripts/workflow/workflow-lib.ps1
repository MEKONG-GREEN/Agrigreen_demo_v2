# Thư viện dùng chung cho scripts/workflow (dot-source: . "$PSScriptRoot\workflow-lib.ps1").
# Phần 1: hàm thuần (phân tích bình luận review, so SHA, quyết định cổng review, đánh giá check CI) — selftest.ps1 kiểm.
# Phần 2: ngữ cảnh máy (repo, thư mục worktree, repo GitHub) và lệnh có tác dụng phụ (in ra ở chế độ -DryRun).
# Phần 3: đọc GitHub (check bắt buộc, chờ check PR, cổng review đọc trực tiếp) — selftest.ps1 kiểm với `gh` giả.

# Tệp sinh: lấy bản của dev khi rebase rồi sinh lại từ nguồn (rebase-merge.ps1); không tính vào nội dung cần review lại.
# CHỈ liệt kê tệp do công cụ ghi lại toàn bộ (modgen, gen:api, AG_UPDATE_WEB_FIXTURES). Tệp nguồn sửa tay — kể cả
# internal/platform/party/domain/menu_departments.go (#98 sửa tay) — KHÔNG được ở đây: xung đột ở đó phải dừng cho chủ gói.
$script:AgGeneratedPathRegex = '(_gen\.go|contracts/openapi/(application|admin)\.yaml|apps/web-ops/src/api/generated/.+|apps/web-ops/src/api/mock/(kn|kn-admin|handler)\.ts|apps/web-ops/src/api/mock/fixtures/kn-menu\.json)$'

# Dòng review chuẩn (ticket.ps1 review sinh ra): "Review OK — Codex (review chéo tại 1a2b3c4)" hoặc "Review CHANGES — …".
# Chấp nhận gạch nối em/en/"-"/"--", SHA trong `…`, in đậm/nghiêng ở đầu dòng (tối đa 3 dấu cách thụt). KHÔNG tính: dòng
# trích dẫn (">"), câu giữa dòng, dòng thụt >= 4 dấu cách hoặc tab (khối code), nội dung trong khối code rào ```/~~~ và
# chú thích HTML (Remove-AgNonVerdictText bỏ trước). "ch\S{1,3}o t\S{1,3}i" chịu được chữ có dấu bị giải mã sai bảng mã.
$script:AgReviewRegex = '(?im)^ {0,3}[*_]{0,3}Review\s+(OK|CHANGES)\s*(?:—|–|-{1,2})\s*(Claude|Codex)\b[^\r\n]*?\(\s*review\s+ch\S{1,3}o\s+t\S{1,3}i\s+`?([0-9a-fA-F]{7,64})`?\s*\)'
# Mẫu yêu cầu sửa cũ đang có trên issue (CODEX-MERGE 24/10 mục 0.3: "Codex review — cần sửa trước Review OK (<sha>).", và
# các biến thể "Codex review … cần sửa …" ở đầu dòng). Chỉ được tính là CHANGES (thu hồi OK cũ), KHÔNG BAO GIỜ là OK:
# phê duyệt luôn phải dùng dòng chuẩn ở trên.
$script:AgLegacyChangesRegex = '(?im)^ {0,3}[*_]{0,3}(Claude|Codex)\s+review\b[^\r\n]*?(?:c\S{1,4}n\s+s\S{1,4}a\b|changes?\s+requested|request(?:ing)?\s+changes)[^\r\n]*'

function Get-AgOtherAgent([string]$Agent) {
    switch ("$Agent".ToLower()) {
        'claude' { return 'codex' }
        'codex' { return 'claude' }
        default { throw "Unknown agent '$Agent' (expected claude|codex)" }
    }
}

function Format-AgReviewLine([ValidateSet('OK', 'CHANGES')][string]$Verdict, [string]$Reviewer, [string]$Sha) {
    $name = (Get-Culture).TextInfo.ToTitleCase("$Reviewer".ToLower())
    return "Review $Verdict $([char]0x2014) $name (review ch$([char]0x00E9)o t$([char]0x1EA1)i $Sha)"
}

# Bỏ phần không phải phán quyết: chú thích HTML và khối code rào (``` hoặc ~~~, đến rào đóng cùng loại; rào không đóng thì
# bỏ tới hết bình luận như CommonMark). Giữ số dòng không quan trọng — chỉ thứ tự.
function Remove-AgNonVerdictText([string]$Body) {
    $t = [regex]::Replace("$Body", '(?s)<!--.*?-->', '')
    $out = New-Object System.Collections.Generic.List[string]
    $fenceChar = $null; $fenceLen = 0
    foreach ($l in ($t -split "\r?\n")) {
        if ($fenceChar) {
            if ($l -match ('^ {0,3}' + [regex]::Escape($fenceChar) + '{' + $fenceLen + ',}\s*$')) { $fenceChar = $null }
            continue
        }
        if ($l -match '^ {0,3}(`{3,}|~{3,})') { $fenceChar = $Matches[1].Substring(0, 1); $fenceLen = $Matches[1].Length; continue }
        $out.Add($l)
    }
    return ($out -join "`n")
}

# Comments: đối tượng có body, createdAt (chuỗi ISO hoặc datetime), url, source tùy chọn. Trả về các phán quyết theo thời gian
# tăng dần (cùng bình luận: theo vị trí). Legacy=$true cho mẫu yêu cầu sửa cũ (luôn CHANGES).
function Get-AgReviewVerdicts([object[]]$Comments) {
    $out = @(); $seq = 0
    foreach ($c in @($Comments)) {
        if ($null -eq $c -or -not $c.body) { continue }
        $at = if ($c.createdAt) { [datetime]$c.createdAt } else { [datetime]::MinValue }
        $text = Remove-AgNonVerdictText ([string]$c.body)
        $hits = @()
        foreach ($m in [regex]::Matches($text, $script:AgReviewRegex)) {
            $hits += [pscustomobject]@{ Index = $m.Index; Verdict = $m.Groups[1].Value.ToUpper(); Reviewer = $m.Groups[2].Value.ToLower(); Sha = $m.Groups[3].Value.ToLower(); Line = $m.Value.Trim(); Legacy = $false }
        }
        foreach ($m in [regex]::Matches($text, $script:AgLegacyChangesRegex)) {
            $sha = ''
            $sm = [regex]::Match($m.Value, '\b([0-9a-fA-F]{7,64})\b')
            if ($sm.Success) { $sha = $sm.Groups[1].Value.ToLower() }
            $hits += [pscustomobject]@{ Index = $m.Index; Verdict = 'CHANGES'; Reviewer = $m.Groups[1].Value.ToLower(); Sha = $sha; Line = $m.Value.Trim(); Legacy = $true }
        }
        foreach ($h in @($hits | Sort-Object Index)) {
            $seq++
            $out += [pscustomobject]@{
                Verdict = $h.Verdict; Reviewer = $h.Reviewer; Sha = $h.Sha; CreatedAt = $at; Seq = $seq
                Url = $c.url; Source = $c.source; Line = $h.Line; Legacy = $h.Legacy
            }
        }
    }
    return @($out | Sort-Object CreatedAt, Seq)
}

# SHA trong bình luận (>= 7 ký tự hex, ngắn hoặc đủ) khớp SHA đầy đủ khi là tiền tố của nó.
function Test-AgShaMatch([string]$Short, [string]$Full) {
    if (-not $Short -or -not $Full) { return $false }
    $s = $Short.Trim().ToLower(); $f = $Full.Trim().ToLower()
    if ($s -notmatch '^[0-9a-f]{7,64}$' -or $f -notmatch '^[0-9a-f]{40,64}$') { return $false }
    return $f.StartsWith($s)
}

function Test-AgGeneratedPath([string]$Path) { return ($Path -replace '\\', '/') -match $script:AgGeneratedPathRegex }

# Chủ gói = nhãn agent:<x> duy nhất trên issue (do ticket.ps1 claim gắn). Không có hoặc có cả hai -> lỗi (không đoán).
function Resolve-AgOwner([string[]]$Labels) {
    $o = @(@($Labels) | Where-Object { "$_" -match '^agent:(claude|codex)$' } | ForEach-Object { "$_".Substring(6).ToLower() } | Sort-Object -Unique)
    if ($o.Count -ne 1) { throw "Cannot determine the package owner: expected exactly one agent:claude|agent:codex label, found [$($o -join ', ')]" }
    return $o[0]
}

# Đường dẫn khác nhau giữa hai commit (không dò đổi tên). Lỗi git -> throw (không coi là "không khác").
function Get-AgChangedPaths([string]$Dir, [string]$A, [string]$B) {
    $ErrorActionPreference = 'Continue'
    $p = @(git -C $Dir -c core.quotepath=false diff --no-renames --no-ext-diff --name-only $A $B 2>$null)
    if ($LASTEXITCODE -ne 0) { throw "git diff --name-only $A $B failed in $Dir" }
    return @($p | ForEach-Object { "$_".Trim() } | Where-Object { $_ })
}

function Resolve-AgCommit([string]$Dir, [string]$Rev) {
    $ErrorActionPreference = 'Continue'
    if (-not $Rev) { return $null }
    $r = git -C $Dir rev-parse --verify --quiet "$Rev^{commit}" 2>$null
    if ($LASTEXITCODE -ne 0 -or -not $r) { return $null }
    return "$r".Trim().ToLower()
}

# Review ở SHA $ReviewSha có còn hiệu lực cho đầu PR $Head (đã rebase lên $BaseRef) không:
#   exact      — SHA review là (tiền tố của) $Head;
#   equivalent — so NỘI DUNG tệp, không so diff: gọi P = các tệp không sinh mà gói đổi (so với merge-base với $BaseRef) ở SHA
#                review hoặc ở $Head. Phủ khi (1) dev KHÔNG đổi tệp nào trong P giữa hai merge-base, và (2) mọi tệp trong P
#                giống hệt (blob + mode) giữa SHA review và $Head. Khi đó $Head = base mới + đúng các tệp đã review; chỉ
#                tệp gói không đụng (lấy từ dev) và tệp sinh khác. Dev cũng đổi tệp của gói -> luôn phải review lại.
function Test-AgReviewCovers([string]$Dir, [string]$ReviewSha, [string]$Head, [string]$BaseRef) {
    $ErrorActionPreference = 'Continue'
    $no = { param($r) [pscustomobject]@{ Covered = $false; Mode = 'none'; Reason = $r } }
    if (Test-AgShaMatch $ReviewSha $Head) { return [pscustomobject]@{ Covered = $true; Mode = 'exact'; Reason = "review SHA $ReviewSha = PR head" } }
    if ("$ReviewSha" -notmatch '^[0-9a-fA-F]{7,64}$') { return & $no "review SHA '$ReviewSha' is not a commit id" }
    $full = Resolve-AgCommit $Dir $ReviewSha
    if (-not $full) { return & $no "review SHA $ReviewSha not found (or ambiguous) in local object store" }
    $headFull = Resolve-AgCommit $Dir $Head
    if (-not $headFull) { return & $no "PR head $Head not found in $Dir" }
    $oldBase = Resolve-AgCommit $Dir "$((git -C $Dir merge-base $full $BaseRef 2>$null) | Select-Object -First 1)"
    $newBase = Resolve-AgCommit $Dir "$((git -C $Dir merge-base $headFull $BaseRef 2>$null) | Select-Object -First 1)"
    if (-not $oldBase -or -not $newBase) { return & $no "no merge-base with $BaseRef" }
    try {
        $pkg = @(@(Get-AgChangedPaths $Dir $oldBase $full) + @(Get-AgChangedPaths $Dir $newBase $headFull) | Where-Object { -not (Test-AgGeneratedPath $_) } | Sort-Object -Unique)
        $set = @{}; foreach ($p in $pkg) { $set[$p] = $true }
        $devTouched = @(Get-AgChangedPaths $Dir $oldBase $newBase | Where-Object { $set.ContainsKey($_) })
        $differs = @(Get-AgChangedPaths $Dir $full $headFull | Where-Object { $set.ContainsKey($_) })
    } catch { return & $no "$_" }
    $r10 = $full.Substring(0, 10); $h10 = $headFull.Substring(0, 10)
    if ($devTouched.Count) { return & $no "dev also changed package file(s) since review ${r10}: $($devTouched -join ', ') - re-review required" }
    if ($differs.Count) { return & $no "package file(s) differ between reviewed $r10 and head ${h10}: $($differs -join ', ')" }
    return [pscustomobject]@{ Covered = $true; Mode = 'equivalent'; Reason = "the $($pkg.Count) package file(s) are identical at reviewed $r10 and head $h10; only files the package does not touch (from dev) and generated files differ" }
}

# Cổng review: cần phán quyết của bên KHÁC CHỦ GÓI ($Owner, từ nhãn agent:<x>), không phụ thuộc bên nào chạy gộp. Phán quyết
# CHANGES mới nhất của bên review (kể cả mẫu cũ "… review — cần sửa …") vô hiệu mọi OK cũ hơn; trong các OK sau đó chỉ cần
# một OK phủ $Head (exact hoặc equivalent). Bình luận đứng tên chủ gói (tự review) bị bỏ qua.
function Resolve-AgReviewGate([object[]]$Verdicts, [string]$Owner, [string]$Head, [string]$Dir, [string]$BaseRef = 'origin/dev', [string]$Pkg = '<PKG>') {
    $rev = Get-AgOtherAgent $Owner
    $revName = (Get-Culture).TextInfo.ToTitleCase($rev)
    $mine = @($Verdicts | Where-Object { $_.Reviewer -eq $rev })
    $short = if ($Head.Length -ge 12) { $Head.Substring(0, 12) } else { $Head }
    $ask = "Package owner is $Owner; ask $revName to review head $short and post: ticket.ps1 review -Pkg $Pkg -Agent $rev -Sha $short -Ok"
    if (-not $mine.Count) { return [pscustomobject]@{ Ok = $false; Review = $null; Mode = 'none'; Message = "REVIEW MISSING: no 'Review OK $([char]0x2014) $revName' comment on the issue or PR. $ask" } }
    $lastChanges = -1
    for ($i = 0; $i -lt $mine.Count; $i++) { if ($mine[$i].Verdict -eq 'CHANGES') { $lastChanges = $i } }
    $oks = @(); for ($i = $lastChanges + 1; $i -lt $mine.Count; $i++) { $oks += $mine[$i] }
    if (-not $oks.Count) {
        $c = $mine[$lastChanges]
        return [pscustomobject]@{ Ok = $false; Review = $c; Mode = 'none'; Message = "REVIEW CHANGES: latest verdict from $revName requests changes at $(if ($c.Sha) { $c.Sha } else { '<no sha>' }) ($($c.Url)). $ask" }
    }
    $why = @()
    for ($i = $oks.Count - 1; $i -ge 0; $i--) {
        $cov = try { Test-AgReviewCovers $Dir $oks[$i].Sha $Head $BaseRef } catch { [pscustomobject]@{ Covered = $false; Mode = 'none'; Reason = "$_" } }
        if ($cov.Covered) { return [pscustomobject]@{ Ok = $true; Review = $oks[$i]; Mode = $cov.Mode; Message = "REVIEW OK ($($cov.Mode)): $($oks[$i].Line) $($oks[$i].Url) - $($cov.Reason)" } }
        $why += "$($oks[$i].Sha): $($cov.Reason)"
    }
    return [pscustomobject]@{ Ok = $false; Review = $oks[-1]; Mode = 'none'; Message = "REVIEW STALE: $($why -join '; '). Re-review needed at the new SHA. $ask" }
}

# --- Check CI (fail-closed) -------------------------------------------------------------------------------------------
# Bucket của `gh pr checks --json`: pass | fail | pending | skipping | cancel. CHỈ 'pass' là xanh; skipping, cancel, fail,
# rỗng hay giá trị lạ đều KHÔNG xanh. Tên được miễn (-AllowFailingCheck) là tên chính xác, không regex/ký tự đại diện.
function Assert-AgCheckWaivers([string[]]$Names) {
    foreach ($n in @($Names)) {
        if ("$n".Trim() -eq '') { throw '-AllowFailingCheck: empty check name' }
        if ("$n" -match '[*?\[\]^$|\\]') { throw "-AllowFailingCheck '$n': exact check names only (no wildcard/regex characters)" }
    }
}

# Đánh giá MỘT lần đọc. Trả State OK | WAIT | FAIL, Message, Waived (tên đã miễn), NoChecks.
#   $ReadOk=$false: không đọc được check (lỗi API/mạng) -> WAIT (hết hạn -> FAIL), không bao giờ coi là "không có check".
#   Có $Required: mỗi tên bắt buộc phải có mặt và mọi lần chạy cùng tên đều 'pass'; thiếu/pending -> WAIT; khác -> FAIL.
#     Không cho miễn khi có check bắt buộc.
#   Không có $Required: mọi check báo về phải 'pass', trừ tên được miễn đúng chính xác (và vẫn phải còn >= 1 check xanh
#     không miễn). Không có check nào sau thời gian ân hạn -> FAIL, trừ khi $NoChecksOk (chỉ trước khi có CI; được ghi lại).
function Resolve-AgCheckState([object[]]$Checks, [string[]]$Required, [bool]$ReadOk, [bool]$GraceOver, [bool]$NoChecksOk, [string[]]$AllowFailing) {
    $Checks = @($Checks | Where-Object { $null -ne $_ }); $Required = @($Required | Where-Object { $_ }); $AllowFailing = @($AllowFailing | Where-Object { $_ })
    $r = { param($s, $m, $w = @(), $n = $false) [pscustomobject]@{ State = $s; Message = $m; Waived = @($w); NoChecks = [bool]$n } }
    if (-not $ReadOk) { return & $r 'WAIT' 'cannot read PR checks (API error) - not treated as "no checks"' }
    $green = { param($c) "$($c.bucket)" -ceq 'pass' }
    $pend = { param($c) "$($c.bucket)" -ceq 'pending' }
    if ($Required.Count) {
        if ($AllowFailing.Count) { return & $r 'FAIL' "refusing -AllowFailingCheck while dev has required checks ($($Required -join ', '))" }
        $wait = @(); $bad = @()
        foreach ($q in $Required) {
            $runs = @($Checks | Where-Object { "$($_.name)" -ceq $q })
            if (-not $runs.Count) { $wait += "$q (missing)"; continue }
            foreach ($c in $runs) {
                if (& $green $c) { continue }
                if (& $pend $c) { $wait += "$q (pending)" } else { $bad += "$q ($(if ($c.bucket) { $c.bucket } else { 'unknown' }))" }
            }
        }
        if ($bad.Count) { return & $r 'FAIL' "required check(s) not green: $($bad -join '; ')" }
        if ($wait.Count) { return & $r 'WAIT' "waiting for required: $($wait -join '; ')" }
        return & $r 'OK' "all $($Required.Count) required check(s) pass"
    }
    if (-not $Checks.Count) {
        if (-not $GraceOver) { return & $r 'WAIT' 'no checks reported yet' }
        if ($NoChecksOk) { return & $r 'OK' 'NO-CHECKS-OK: no checks reported on the PR head and dev has no required checks; merging without CI because -NoChecksOk was given explicitly' @() $true }
        return & $r 'FAIL' 'no checks reported on the PR head and dev has no required checks: refusing to merge without CI (-NoChecksOk only before CI exists, see README)'
    }
    $bad = @($Checks | Where-Object { -not (& $green $_) -and -not (& $pend $_) })
    $waived = @($bad | Where-Object { $AllowFailing -ccontains "$($_.name)" })
    $hard = @($bad | Where-Object { $AllowFailing -cnotcontains "$($_.name)" })
    if ($hard.Count) { return & $r 'FAIL' "check(s) not green: $(($hard | ForEach-Object { "$($_.name) ($(if ($_.bucket) { $_.bucket } else { 'unknown' }))" }) -join '; ')" }
    $pending = @($Checks | Where-Object { & $pend $_ })
    if ($pending.Count) { return & $r 'WAIT' "pending: $(($pending | ForEach-Object { $_.name }) -join '; ')" }
    $passing = @($Checks | Where-Object { (& $green $_) -and $AllowFailing -cnotcontains "$($_.name)" })
    if (-not $passing.Count) { return & $r 'FAIL' 'no passing check left after waivers: refusing to merge' }
    $wn = @($waived | ForEach-Object { "$($_.name) ($($_.bucket))" })
    $msg = "all $($passing.Count) non-waived check(s) pass"
    if ($wn.Count) { $msg += "; WAIVED (explicit -AllowFailingCheck): $($wn -join '; ')" }
    return & $r 'OK' $msg $wn
}

# Chọn toolchain Go chuẩn cho cổng cục bộ: AG_GO_TOOLCHAIN, rồi GOTOOLCHAIN đặt rõ (goX.Y[.Z]), rồi chỉ thị `toolchain` trong
# go.mod. Không có -> $null (người gọi từ chối chạy cổng). 'auto'/'local' không tính là chỉ định.
function Resolve-AgGoToolchain([string]$AgEnv, [string]$GoEnv, [string]$GoModText) {
    $re = '^go\d+\.\d+(\.\d+)?$'
    if ("$AgEnv".Trim() -match $re) { return [pscustomobject]@{ Version = "$AgEnv".Trim(); Source = 'AG_GO_TOOLCHAIN' } }
    $g = ("$GoEnv".Trim() -split '\+')[0]
    if ($g -match $re) { return [pscustomobject]@{ Version = $g; Source = 'GOTOOLCHAIN' } }
    $m = [regex]::Match("$GoModText", '(?m)^toolchain\s+(go\d+\.\d+(?:\.\d+)?)\s*$')
    if ($m.Success) { return [pscustomobject]@{ Version = $m.Groups[1].Value; Source = 'go.mod toolchain' } }
    return $null
}

# ---------------------------------------------------------------------------------------------------------------------
# Ngữ cảnh máy. Ghi đè bằng biến môi trường: AG_REPO_ROOT, AG_WORKTREE_ROOT, AG_GH_REPO, AG_HANDOFF_DIR.
function Get-AgContext([string]$ScriptDir) {
    $ErrorActionPreference = 'Continue'
    $repo = $env:AG_REPO_ROOT
    if (-not $repo) {
        # Bản checkout chính (cha của thư mục .git dùng chung), kể cả khi script chạy từ một worktree.
        $common = git -C $ScriptDir rev-parse --path-format=absolute --git-common-dir 2>$null
        if ($LASTEXITCODE -ne 0 -or -not $common) { throw "Cannot locate the git repository from $ScriptDir; set AG_REPO_ROOT" }
        $common = [IO.Path]::GetFullPath("$common".Trim())
        $repo = if ((Split-Path -Leaf $common) -eq '.git') { Split-Path -Parent $common } else { [IO.Path]::GetFullPath("$(git -C $ScriptDir rev-parse --show-toplevel)".Trim()) }
    }
    $wt = if ($env:AG_WORKTREE_ROOT) { $env:AG_WORKTREE_ROOT } else { Join-Path $env:USERPROFILE 'agwt' }
    $gh = $env:AG_GH_REPO
    if (-not $gh) {
        Push-Location $repo
        try { $gh = "$(gh repo view --json nameWithOwner -q .nameWithOwner 2>$null)".Trim() } finally { Pop-Location }
        if (-not $gh) { throw 'Cannot determine the GitHub repository (gh repo view); set AG_GH_REPO=owner/name' }
    }
    $handoff = $env:AG_HANDOFF_DIR
    if (-not $handoff) {
        $sib = Join-Path (Split-Path -Parent $repo) 'kn-handoff'
        $handoff = if (Test-Path -LiteralPath $sib) { $sib } else { Join-Path $env:TEMP 'ag-workflow' }
    }
    if (-not (Test-Path -LiteralPath $handoff)) { New-Item -ItemType Directory -Force -Path $handoff | Out-Null }
    return [pscustomobject]@{ Repo = $repo; WorktreeRoot = $wt; GhRepo = $gh; HandoffDir = $handoff }
}

$script:AgDryRun = $false
function Format-AgCmd([string]$Exe, [string[]]$ArgList) {
    return (@($Exe) + @($ArgList | ForEach-Object { if ("$_" -match '[\s"]' -or "$_" -eq '') { '"' + ("$_" -replace '"', '\"') + '"' } else { "$_" } })) -join ' '
}
# Lệnh có tác dụng phụ ra ngoài (push, gh pr/issue …): -DryRun chỉ in, không chạy, coi như thành công.
function Invoke-AgMut([string]$Exe, [string[]]$ArgList) {
    $ErrorActionPreference = 'Continue'
    if ($script:AgDryRun) { "[dry-run] $(Format-AgCmd $Exe $ArgList)"; $global:LASTEXITCODE = 0; return }
    "+ $(Format-AgCmd $Exe $ArgList)"
    & $Exe @ArgList 2>&1 | ForEach-Object { "  $_" }
}
# Lệnh chỉ đọc: luôn chạy, trả stdout (stderr bỏ), in lệnh khi -DryRun để thấy toàn bộ kế hoạch.
function Invoke-AgRead([string]$Exe, [string[]]$ArgList) {
    $ErrorActionPreference = 'Continue'
    if ($script:AgDryRun) { Write-Host "[read] $(Format-AgCmd $Exe $ArgList)" }
    & $Exe @ArgList 2>$null
}

# Bình luận của issue và PR (nếu có) theo dạng {body, createdAt, url, source} cho Get-AgReviewVerdicts.
# Lỗi đọc -> throw (fail-closed): một CHANGES không đọc được không được coi là "không có".
function Get-AgThreadComments([string]$GhRepo, [int]$Issue, [string]$Pr) {
    $all = @()
    if ($Issue) {
        $x = Invoke-AgGh @('issue', 'view', "$Issue", '-R', $GhRepo, '--json', 'comments')
        if ($x.Rc -ne 0 -or -not $x.Out.StartsWith('{')) { throw "cannot read comments of issue #$Issue (rc=$($x.Rc)): $($x.Err)" }
        foreach ($c in @(($x.Out | ConvertFrom-Json).comments)) { if ($c) { $all += [pscustomobject]@{ body = $c.body; createdAt = $c.createdAt; url = $c.url; source = "issue #$Issue" } } }
    }
    if ($Pr) {
        $x = Invoke-AgGh @('pr', 'view', "$Pr", '-R', $GhRepo, '--json', 'comments,reviews')
        if ($x.Rc -ne 0 -or -not $x.Out.StartsWith('{')) { throw "cannot read comments of PR #$Pr (rc=$($x.Rc)): $($x.Err)" }
        $o = $x.Out | ConvertFrom-Json
        foreach ($c in @($o.comments)) { if ($c) { $all += [pscustomobject]@{ body = $c.body; createdAt = $c.createdAt; url = $c.url; source = "PR #$Pr" } } }
        foreach ($c in @($o.reviews)) { if ($c -and $c.body) { $all += [pscustomobject]@{ body = $c.body; createdAt = $c.submittedAt; url = "PR #$Pr review"; source = "PR #$Pr" } } }
    }
    return $all
}

# PR mở vào dev có head pr/<PKG>: đối tượng {number,url,headRefOid} hoặc $null. Lỗi đọc -> throw.
function Get-AgOpenPr([string]$GhRepo, [string]$HeadBranch) {
    $x = Invoke-AgGh @('pr', 'list', '-R', $GhRepo, '--head', $HeadBranch, '--base', 'dev', '--state', 'open', '--json', 'number,url,headRefOid')
    if ($x.Rc -ne 0 -or -not $x.Out.StartsWith('[')) { throw "cannot list PRs for $HeadBranch (rc=$($x.Rc)): $($x.Err)" }
    $arr = $x.Out | ConvertFrom-Json
    foreach ($p in $arr) { if ($p) { return $p } }
    return $null
}

# ---------------------------------------------------------------------------------------------------------------------
# Phần 3: đọc GitHub, tách stdout/stderr và mã thoát để lỗi đọc không bị hiểu nhầm là "không có gì".
function Invoke-AgGh([string[]]$ArgList) {
    $ErrorActionPreference = 'Continue'
    if ($script:AgDryRun) { Write-Host "[read] $(Format-AgCmd gh $ArgList)" }
    $global:LASTEXITCODE = 0
    $all = @(& gh @ArgList 2>&1); $rc = $LASTEXITCODE
    $isErr = { param($x) $x -is [System.Management.Automation.ErrorRecord] }
    $out = (@($all | Where-Object { -not (& $isErr $_) } | ForEach-Object { "$_" }) -join "`n").Trim()
    $err = (@($all | Where-Object { & $isErr $_ } | ForEach-Object { "$_" }) -join "`n").Trim()
    return [pscustomobject]@{ Rc = $rc; Out = $out; Err = $err }
}

# Check bắt buộc của nhánh (bảo vệ nhánh cổ điển + ruleset). Ok=$false khi không đọc được (fail-closed: người gọi dừng).
# Chỉ 404 "Branch not protected"/"Required status checks not enabled" được coi là "không có".
function Get-AgRequiredChecks([string]$GhRepo, [string]$Branch = 'dev') {
    $names = @()
    $p = Invoke-AgGh @('api', "repos/$GhRepo/branches/$Branch/protection/required_status_checks")
    if ($p.Rc -eq 0) {
        try { $o = $p.Out | ConvertFrom-Json } catch { return [pscustomobject]@{ Ok = $false; Names = @(); Error = "protection: unparsable response" } }
        $names += @($o.contexts | Where-Object { $_ }); $names += @($o.checks | ForEach-Object { $_.context } | Where-Object { $_ })
    } elseif ("$($p.Err) $($p.Out)" -notmatch 'Branch not protected|Required status checks not enabled') {
        return [pscustomobject]@{ Ok = $false; Names = @(); Error = "protection read failed (rc=$($p.Rc)): $($p.Err)" }
    }
    $q = Invoke-AgGh @('api', "repos/$GhRepo/rules/branches/$Branch")
    if ($q.Rc -ne 0) { return [pscustomobject]@{ Ok = $false; Names = @(); Error = "rules read failed (rc=$($q.Rc)): $($q.Err)" } }
    try { $rules = @($q.Out | ConvertFrom-Json | ForEach-Object { $_ }) } catch { return [pscustomobject]@{ Ok = $false; Names = @(); Error = 'rules: unparsable response' } }
    foreach ($ru in $rules) { if ($ru.type -eq 'required_status_checks') { $names += @($ru.parameters.required_status_checks | ForEach-Object { $_.context } | Where-Object { $_ }) } }
    return [pscustomobject]@{ Ok = $true; Names = @($names | Sort-Object -Unique); Error = '' }
}

# Một lần đọc check của PR. `gh pr checks` thoát 8 khi còn pending, 1 khi có check đỏ — vẫn in JSON; nên quyết định theo
# stdout: JSON mảng -> đọc được; "no checks reported" -> 0 check; còn lại -> lỗi đọc.
function Read-AgPrChecks([string]$GhRepo, [string]$Pr, [bool]$RequiredOnly) {
    $a = @('pr', 'checks', $Pr, '-R', $GhRepo, '--json', 'name,bucket,state'); if ($RequiredOnly) { $a += '--required' }
    $x = Invoke-AgGh $a
    if ($x.Out.StartsWith('[')) {
        try { return [pscustomobject]@{ Ok = $true; Checks = @($x.Out | ConvertFrom-Json | ForEach-Object { $_ }); Error = '' } } catch { }
    }
    if ("$($x.Err) $($x.Out)" -match 'no (required )?checks reported') { return [pscustomobject]@{ Ok = $true; Checks = @(); Error = '' } }
    return [pscustomobject]@{ Ok = $false; Checks = @(); Error = "gh pr checks rc=$($x.Rc): $($x.Err)" }
}

# Chờ check của PR trên đúng SHA $Head (fail-closed). Result OK | FAIL; Message; Waived; NoChecks; Checks (lần đọc cuối).
# Đầu PR khác $Head -> FAIL ngay. Lỗi đọc liên tục tới hạn -> FAIL (không bao giờ OK).
function Wait-AgPrChecks([string]$GhRepo, [string]$Pr, [string]$Head, [string[]]$Required, [int]$TimeoutSec, [int]$GraceSec, [int]$PollSec, [bool]$NoChecksOk, [string[]]$AllowFailing) {
    $sw = [Diagnostics.Stopwatch]::StartNew()
    $last = 'no read yet'; $checks = @()
    while ($true) {
        $v = Invoke-AgGh @('pr', 'view', $Pr, '-R', $GhRepo, '--json', 'headRefOid', '-q', '.headRefOid')
        $readOk = $true
        if ($v.Rc -ne 0 -or $v.Out -notmatch '^[0-9a-fA-F]{40,64}$') { $readOk = $false; $last = "cannot read PR head (rc=$($v.Rc)): $($v.Err)" }
        elseif ($v.Out.ToLower() -ne $Head.ToLower()) {
            return [pscustomobject]@{ Result = 'FAIL'; Message = "PR head moved to $($v.Out) (expected $Head)"; Waived = @(); NoChecks = $false; Checks = @() }
        }
        $st = $null
        if ($readOk) {
            $rd = Read-AgPrChecks $GhRepo $Pr ([bool]@($Required).Count)
            $checks = @($rd.Checks)
            $st = Resolve-AgCheckState $checks $Required $rd.Ok ($sw.Elapsed.TotalSeconds -ge $GraceSec) $NoChecksOk $AllowFailing
            $last = if ($rd.Ok) { $st.Message } else { $rd.Error }
            if ($st.State -ne 'WAIT') {
                return [pscustomobject]@{ Result = $st.State; Message = $st.Message; Waived = $st.Waived; NoChecks = $st.NoChecks; Checks = $checks }
            }
        }
        if ($sw.Elapsed.TotalSeconds -ge $TimeoutSec) {
            return [pscustomobject]@{ Result = 'FAIL'; Message = "TIMEOUT after ${TimeoutSec}s (last: $last)"; Waived = @(); NoChecks = $false; Checks = $checks }
        }
        Start-Sleep -Seconds $PollSec
    }
}

# Cổng review đọc trực tiếp từ issue/PR (dùng trước cổng cục bộ VÀ ngay trước gh pr merge). Lỗi đọc -> không có bình luận
# -> REVIEW MISSING (fail-closed).
function Get-AgLiveReviewGate([string]$GhRepo, [int]$Issue, [string]$Pr, [string]$Owner, [string]$Head, [string]$Dir, [string]$BaseRef, [string]$Pkg) {
    try { $comments = Get-AgThreadComments $GhRepo $Issue $Pr }
    catch { return [pscustomobject]@{ Ok = $false; Review = $null; Mode = 'none'; Message = "REVIEW READ ERROR: $_" } }
    return Resolve-AgReviewGate @(Get-AgReviewVerdicts $comments) $Owner $Head $Dir $BaseRef $Pkg
}
