# Thư viện dùng chung cho scripts/workflow (dot-source: . "$PSScriptRoot\workflow-lib.ps1").
# Phần 1: hàm thuần (phân tích bình luận review, so SHA, quyết định cổng review) — selftest.ps1 kiểm các hàm này.
# Phần 2: ngữ cảnh máy (repo, thư mục worktree, repo GitHub) và lệnh có tác dụng phụ (in ra ở chế độ -DryRun).

# Tệp sinh: lấy bản của dev khi rebase rồi sinh lại từ nguồn (rebase-merge.ps1); không tính vào nội dung cần review lại.
$script:AgGeneratedPathRegex = '(_gen\.go|internal/platform/party/domain/(menu_departments|kn_test)\.go|contracts/openapi/(application|admin)\.yaml|apps/web-ops/src/api/generated/.+|apps/web-ops/src/api/mock/(kn|kn-admin|handler)\.ts|apps/web-ops/src/api/mock/fixtures/kn-menu\.json)$'

# Dòng review chuẩn (ticket.ps1 review sinh ra): "Review OK — Codex (review chéo tại 1a2b3c4)" hoặc "Review CHANGES — …".
# Chấp nhận gạch nối em/en/"-"/"--", SHA trong `…`, in đậm/nghiêng ở đầu dòng; dòng trích dẫn (">") hoặc câu giữa dòng KHÔNG tính.
# "ch\S{1,3}o t\S{1,3}i" chịu được chữ có dấu bị giải mã sai bảng mã.
$script:AgReviewRegex = '(?im)^[ \t*_]*Review\s+(OK|CHANGES)\s*(?:—|–|-{1,2})\s*(Claude|Codex)\b[^\r\n]*?\(\s*review\s+ch\S{1,3}o\s+t\S{1,3}i\s+`?([0-9a-fA-F]{7,64})`?\s*\)'

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

# Comments: đối tượng có body, createdAt (chuỗi ISO hoặc datetime), url, source tùy chọn. Trả về các phán quyết theo thời gian tăng dần.
function Get-AgReviewVerdicts([object[]]$Comments) {
    $out = @(); $seq = 0
    foreach ($c in @($Comments)) {
        if ($null -eq $c -or -not $c.body) { continue }
        $at = if ($c.createdAt) { [datetime]$c.createdAt } else { [datetime]::MinValue }
        foreach ($m in [regex]::Matches([string]$c.body, $script:AgReviewRegex)) {
            $seq++
            $out += [pscustomobject]@{
                Verdict = $m.Groups[1].Value.ToUpper(); Reviewer = $m.Groups[2].Value.ToLower(); Sha = $m.Groups[3].Value.ToLower()
                CreatedAt = $at; Seq = $seq; Url = $c.url; Source = $c.source; Line = $m.Value.Trim()
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

# "Phần thay đổi của gói" giữa base và head, bỏ tệp sinh: diff -U0 không ngữ cảnh, bỏ dòng index/@@ (phụ thuộc base và số
# dòng), tệp nhị phân thay bằng SHA blob sau. Hai lần rebase sạch cho cùng chuỗi thì trùng nhau.
function Get-AgPackageDelta([string]$Dir, [string]$Base, [string]$Head) {
    $ErrorActionPreference = 'Continue'
    $lines = @(git -C $Dir -c core.quotepath=false diff --no-color --no-ext-diff --no-textconv --no-renames --full-index -U0 $Base $Head 2>$null)
    if ($LASTEXITCODE -ne 0) { throw "git diff $Base $Head failed in $Dir" }
    $sb = New-Object System.Text.StringBuilder
    $keep = $false; $post = ''
    foreach ($l in $lines) {
        $l = "$l"
        if ($l -match '^diff --git a/(.+) b/(.+)$') {
            $keep = -not (Test-AgGeneratedPath $Matches[2]); $post = ''
            if ($keep) { [void]$sb.AppendLine($l) }
            continue
        }
        if (-not $keep) { continue }
        if ($l -match '^index [0-9a-f]+\.\.([0-9a-f]+)') { $post = $Matches[1]; continue }
        if ($l.StartsWith('@@')) { continue }
        if ($l -match '^Binary files .* differ$') { [void]$sb.AppendLine("binary $post"); continue }
        [void]$sb.AppendLine($l)
    }
    return $sb.ToString()
}

function Resolve-AgCommit([string]$Dir, [string]$Rev) {
    $ErrorActionPreference = 'Continue'
    $r = git -C $Dir rev-parse --verify --quiet "$Rev^{commit}" 2>$null
    if ($LASTEXITCODE -ne 0 -or -not $r) { return $null }
    return "$r".Trim().ToLower()
}

# Review ở SHA $ReviewSha có còn hiệu lực cho đầu PR $Head (đã rebase lên $BaseRef) không:
#   exact      — SHA review là (tiền tố của) $Head;
#   equivalent — phần thay đổi ngoài tệp sinh của gói tại SHA review (so với merge-base của nó với $BaseRef) giống hệt
#                phần thay đổi tại $Head (so với merge-base $Head/$BaseRef): rebase/sinh lại không đổi nội dung đã review.
function Test-AgReviewCovers([string]$Dir, [string]$ReviewSha, [string]$Head, [string]$BaseRef) {
    $ErrorActionPreference = 'Continue'
    if (Test-AgShaMatch $ReviewSha $Head) { return [pscustomobject]@{ Covered = $true; Mode = 'exact'; Reason = "review SHA $ReviewSha = PR head" } }
    $full = Resolve-AgCommit $Dir $ReviewSha
    if (-not $full) { return [pscustomobject]@{ Covered = $false; Mode = 'none'; Reason = "review SHA $ReviewSha not found (or ambiguous) in local object store" } }
    $oldBase = Resolve-AgCommit $Dir ((git -C $Dir merge-base $full $BaseRef 2>$null) | Select-Object -First 1)
    $newBase = Resolve-AgCommit $Dir ((git -C $Dir merge-base $Head $BaseRef 2>$null) | Select-Object -First 1)
    if (-not $oldBase -or -not $newBase) { return [pscustomobject]@{ Covered = $false; Mode = 'none'; Reason = "no merge-base with $BaseRef" } }
    $a = Get-AgPackageDelta $Dir $oldBase $full
    $b = Get-AgPackageDelta $Dir $newBase $Head
    if ($a -ceq $b) {
        return [pscustomobject]@{ Covered = $true; Mode = 'equivalent'; Reason = "review SHA $($full.Substring(0,10)) differs from head only by rebase base and generated files" }
    }
    return [pscustomobject]@{ Covered = $false; Mode = 'none'; Reason = "non-generated content changed between reviewed $($full.Substring(0,10)) and head $($Head.Substring(0,10))" }
}

# Cổng review: bên gộp $MergingAgent cần phán quyết của bên kia. Phán quyết CHANGES mới nhất của bên kia vô hiệu mọi OK cũ
# hơn; trong các OK sau đó, chỉ cần một OK phủ $Head (exact hoặc equivalent). Bình luận của chính bên gộp bị bỏ qua.
function Resolve-AgReviewGate([object[]]$Verdicts, [string]$MergingAgent, [string]$Head, [string]$Dir, [string]$BaseRef = 'origin/dev', [string]$Pkg = '<PKG>') {
    $rev = Get-AgOtherAgent $MergingAgent
    $revName = (Get-Culture).TextInfo.ToTitleCase($rev)
    $mine = @($Verdicts | Where-Object { $_.Reviewer -eq $rev })
    $short = if ($Head.Length -ge 12) { $Head.Substring(0, 12) } else { $Head }
    $ask = "Ask $revName to review head $short and post: ticket.ps1 review -Pkg $Pkg -Agent $rev -Sha $short -Ok"
    if (-not $mine.Count) { return [pscustomobject]@{ Ok = $false; Review = $null; Mode = 'none'; Message = "REVIEW MISSING: no 'Review OK $([char]0x2014) $revName' comment on the issue or PR. $ask" } }
    $lastChanges = -1
    for ($i = 0; $i -lt $mine.Count; $i++) { if ($mine[$i].Verdict -eq 'CHANGES') { $lastChanges = $i } }
    $oks = @(); for ($i = $lastChanges + 1; $i -lt $mine.Count; $i++) { $oks += $mine[$i] }
    if (-not $oks.Count) {
        $c = $mine[$lastChanges]
        return [pscustomobject]@{ Ok = $false; Review = $c; Mode = 'none'; Message = "REVIEW CHANGES: latest verdict from $revName requests changes at $($c.Sha) ($($c.Url)). $ask" }
    }
    $why = @()
    for ($i = $oks.Count - 1; $i -ge 0; $i--) {
        $cov = Test-AgReviewCovers $Dir $oks[$i].Sha $Head $BaseRef
        if ($cov.Covered) { return [pscustomobject]@{ Ok = $true; Review = $oks[$i]; Mode = $cov.Mode; Message = "REVIEW OK ($($cov.Mode)): $($oks[$i].Line) $($oks[$i].Url) - $($cov.Reason)" } }
        $why += "$($oks[$i].Sha): $($cov.Reason)"
    }
    return [pscustomobject]@{ Ok = $false; Review = $oks[-1]; Mode = 'none'; Message = "REVIEW STALE: $($why -join '; '). Re-review needed at the new SHA. $ask" }
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
function Get-AgThreadComments([string]$GhRepo, [int]$Issue, [string]$Pr) {
    $all = @()
    if ($Issue) {
        $j = Invoke-AgRead gh @('issue', 'view', "$Issue", '-R', $GhRepo, '--json', 'comments') | Out-String
        if ($j.Trim()) { foreach ($c in @(($j | ConvertFrom-Json).comments)) { $all += [pscustomobject]@{ body = $c.body; createdAt = $c.createdAt; url = $c.url; source = "issue #$Issue" } } }
    }
    if ($Pr) {
        $j = Invoke-AgRead gh @('pr', 'view', "$Pr", '-R', $GhRepo, '--json', 'comments,reviews') | Out-String
        if ($j.Trim()) {
            $o = $j | ConvertFrom-Json
            foreach ($c in @($o.comments)) { $all += [pscustomobject]@{ body = $c.body; createdAt = $c.createdAt; url = $c.url; source = "PR #$Pr" } }
            foreach ($c in @($o.reviews)) { if ($c.body) { $all += [pscustomobject]@{ body = $c.body; createdAt = $c.submittedAt; url = "PR #$Pr review"; source = "PR #$Pr" } } }
        }
    }
    return $all
}

# PR mở vào dev có head pr/<PKG>: số PR hoặc ''.
function Get-AgOpenPr([string]$GhRepo, [string]$HeadBranch) {
    $j = Invoke-AgRead gh @('pr', 'list', '-R', $GhRepo, '--head', $HeadBranch, '--base', 'dev', '--state', 'open', '--json', 'number,url,headRefOid') | Out-String
    if (-not $j.Trim()) { return $null }
    $arr = $j | ConvertFrom-Json
    foreach ($p in $arr) { return $p }
    return $null
}
