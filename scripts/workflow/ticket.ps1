param(
    [Parameter(Mandatory, Position = 0)][ValidateSet('list', 'claim', 'ready', 'review', 'release', 'merge', 'lock-status')][string]$Cmd,
    [string]$Pkg,
    [ValidateSet('claude', 'codex')][string]$Agent,
    [ValidateSet('kn', 'shared', '')][string]$Lane = '',
    [string]$Worktree,          # folder under AG_WORKTREE_ROOT (default %USERPROFILE%\agwt; folder default: package name lower-cased without dots)
    [string]$Tests = '',        # comma-separated go test patterns for merge
    [string]$Note = '',
    [string]$Sha = '',          # review: commit reviewed (short >= 7 or full)
    [switch]$Ok,                # review: approve
    [switch]$Changes,           # review: request changes
    [int]$ChecksTimeoutMin = 0, # merge: wait for PR checks (default AG_PR_CHECKS_TIMEOUT_MIN or 90)
    [switch]$DryRun             # merge/review/claim/ready/release: print git/gh side effects instead of running them
)
# Ticket workflow shared by Claude Code and Codex (scripts/workflow/README.md, kn-handoff/TWO-AGENT.md).
#   list   [-Lane kn|shared]                      open st:ready issues (and who holds what)
#   claim  -Pkg P -Agent A                        atomic: push refs/heads/claim/P (no force) -> st:claimed + agent:A
#   ready  -Pkg P [-Note "checks..."]             st:claimed -> st:review (+ comment)
#   review -Pkg P -Agent A -Sha S -Ok|-Changes [-Note ...]   reviewer A posts "Review OK|CHANGES — A (review chéo tại S)"
#   release -Pkg P [-Note why]                    delete claim/P -> st:ready
#   merge  -Pkg P -Agent A [-Worktree w] [-Tests a,b] [-DryRun]
#                                                 A = agent running the merge (owner or reviewer). The review must come from
#                                                 the agent OTHER than the package owner (issue label agent:<owner>).
#                                                 review pre-check -> atomic lock/integration -> rebase-merge.ps1
#                                                 (rebase, regen, review gate, gates, PR pr/P -> dev, fail-closed checks,
#                                                 review re-check, merge) -> st:done + close, always unlock
#   lock-status                                   who holds lock/integration
$ErrorActionPreference = 'Stop'
$env:GIT_TERMINAL_PROMPT = '0'; $env:GCM_INTERACTIVE = 'never'   # never block on a credential dialog
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
$sp = Split-Path -Parent $MyInvocation.MyCommand.Path
. (Join-Path $sp 'workflow-lib.ps1')
$script:AgDryRun = [bool]$DryRun
$ctx = Get-AgContext $sp
$repo = $ctx.Repo
$gh = $ctx.GhRepo

function RepoRev([string]$r) { (git -C $repo rev-parse $r).Trim() }
function IssueObj([string]$p) {
    $all = gh issue list -R $gh --state open --limit 200 --json number,title,labels | ConvertFrom-Json
    $hit = @($all | Where-Object { $_.title.StartsWith("[$p]") })
    if ($hit.Count -ne 1) { throw "No unique open issue titled [$p]" }
    return $hit[0]
}
function Issue([string]$p) { return (IssueObj $p).number }
function Labels([int]$n, [string[]]$add, [string[]]$remove) {
    $a = @('issue', 'edit', "$n", '-R', $gh); foreach ($l in $add) { $a += @('--add-label', $l) }; foreach ($l in $remove) { $a += @('--remove-label', $l) }
    Invoke-AgMut gh $a | Out-Host
}
function Comment([int]$n, [string]$text) {
    if ($script:AgDryRun) { "[dry-run] gh issue comment $n -R $gh --body <<"; $text -split "`n" | ForEach-Object { "    $_" }; return }
    gh issue comment $n -R $gh --body $text | Out-Null
}
# git prints "remote: ..." on stderr even on success; under 'Stop' PowerShell 5.1 would treat it as fatal.
# CreateRef is atomic create-only: it pushes a fresh commit (unique per holder and time) with an empty lease, so the push
# fails whenever the ref already exists — even if it points at the same commit (a plain push of an identical sha reports
# "Everything up-to-date" and let two agents both believe they held the lock on 21/10/2026). Returns the new sha or $null.
function CreateRef([string]$ref, [string]$base, [string]$holder) {
    $ErrorActionPreference = 'Continue'
    if ($script:AgDryRun) { "[dry-run] git push origin --force-with-lease=refs/heads/${ref}: <commit-tree of $base>:refs/heads/$ref" | Out-Host; return 'dry-run' }
    $msg = "$ref held by $holder on $env:COMPUTERNAME at $(Get-Date -Format o)"
    $sha = (git -C $repo commit-tree "$base^{tree}" -p $base -m $msg 2>$null)
    if (-not $sha) { return $null }
    $sha = "$sha".Trim()
    git -C $repo push -q origin "--force-with-lease=refs/heads/${ref}:" "${sha}:refs/heads/$ref" 2>&1 | Out-Null
    if ($LASTEXITCODE -eq 0) { return $sha } else { return $null }
}
# DeleteRef removes the ref only while it still points at $sha (our own lock/claim); without $sha it deletes unconditionally.
function DeleteRef([string]$ref, [string]$sha = '') {
    $ErrorActionPreference = 'Continue'
    if ($script:AgDryRun) { "[dry-run] git push origin --delete $ref$(if ($sha) { " (lease $sha)" })" | Out-Host; return }
    if ($sha) { git -C $repo push -q origin "--force-with-lease=refs/heads/${ref}:$sha" --delete $ref 2>&1 | Out-Null }
    else { git -C $repo push -q origin --delete $ref 2>&1 | Out-Null }
}

switch ($Cmd) {
    'list' {
        $q = 'label:st:ready'; if ($Lane) { $q += " label:lane:$Lane" }
        gh issue list -R $gh --state open --search $q --limit 100 --json number,title,labels |
            ConvertFrom-Json | Sort-Object number | ForEach-Object { '#{0} {1}  [{2}]' -f $_.number, $_.title, (($_.labels.name | Where-Object { $_ -match '^(size|phong):' }) -join ' ') }
        '--- claims:'; git -C $repo ls-remote --heads origin 'claim/*' | ForEach-Object { ($_ -split 'refs/heads/')[1] }
        '--- lock:'; git -C $repo ls-remote --heads origin 'lock/integration'
    }
    'claim' {
        if (-not $Pkg -or -not $Agent) { throw 'claim needs -Pkg and -Agent' }
        $n = Issue $Pkg
        git -C $repo fetch -q origin +refs/heads/dev:refs/remotes/origin/dev
        if (-not (CreateRef "claim/$Pkg" (RepoRev origin/dev) $Agent)) { "ALREADY CLAIMED: claim/$Pkg exists"; exit 3 }
        Labels $n @('st:claimed', "agent:$Agent") @('st:ready')
        Comment $n "Nhận bởi **$Agent** lúc $(Get-Date -Format 'dd/MM/yyyy HH:mm'). $Note"
        "claimed #$n $Pkg"
    }
    'ready' {
        $n = Issue $Pkg
        Labels $n @('st:review') @('st:claimed')
        Comment $n "Sẵn sàng gộp. $Note"
        "ready #$n $Pkg"
    }
    'review' {
        if (-not $Pkg -or -not $Agent -or -not $Sha) { throw 'review needs -Pkg, -Agent (the reviewer) and -Sha' }
        if ([bool]$Ok -eq [bool]$Changes) { throw 'review needs exactly one of -Ok or -Changes' }
        if ($Sha -notmatch '^[0-9a-fA-F]{7,64}$') { throw "-Sha must be 7..64 hex characters, got '$Sha'" }
        $iss = IssueObj $Pkg
        $owner = @($iss.labels | ForEach-Object { $_.name } | Where-Object { $_ -like 'agent:*' } | ForEach-Object { $_.Substring(6) })
        if ($owner -contains $Agent) { throw "Self-review refused: #$($iss.number) is labelled agent:$Agent; the review must come from $(Get-AgOtherAgent $Agent)" }
        $full = Resolve-AgCommit $repo $Sha
        if (-not $full) { Write-Warning "commit $Sha not found locally (fetch the package branch?); posting anyway" }
        $line = Format-AgReviewLine $(if ($Ok) { 'OK' } else { 'CHANGES' }) $Agent $Sha.ToLower()
        $text = if ($Note) { "$line`n`n$Note" } else { $line }
        $pr = Get-AgOpenPr $gh "pr/$Pkg"
        Comment $iss.number $text
        "$(if ($DryRun) { '[dry-run] would post review' } else { 'review posted' }) on #$($iss.number) $Pkg$(if ($pr) { " (open PR #$($pr.number) head $($pr.headRefOid.Substring(0,12)))" }): $line"
    }
    'release' {
        $n = Issue $Pkg
        DeleteRef "claim/$Pkg"
        Labels $n @('st:ready') @('st:claimed', 'agent:claude', 'agent:codex')
        Comment $n "Trả gói. $Note"
        "released #$n $Pkg"
    }
    'lock-status' { git -C $repo ls-remote --heads origin 'lock/integration' }
    'merge' {
        if (-not $Pkg -or -not $Agent) { throw 'merge needs -Pkg and -Agent (the agent running the merge)' }
        $iss = IssueObj $Pkg; $n = $iss.number
        # Owner from the claim label, never from who runs the merge (CODEX-MERGE: Codex reviews then merges Claude packages).
        try { $owner = Resolve-AgOwner @($iss.labels | ForEach-Object { $_.name }) } catch { "OWNER UNKNOWN on #${n}: $_"; exit 4 }
        $reviewer = Get-AgOtherAgent $owner
        if (-not $Worktree) { $Worktree = ($Pkg.ToLower() -replace '[^a-z0-9]', '') }
        git -C $repo fetch -q origin +refs/heads/dev:refs/remotes/origin/dev
        # Pre-check before taking the lock: the latest verdict of the reviewer (other than the owner) must be OK (any SHA).
        # The exact SHA rule is enforced by rebase-merge.ps1 once the PR head (after rebase/regeneration) is known, and
        # re-checked right before gh pr merge.
        "owner: $owner (label agent:$owner); reviewer: $reviewer; merging agent: $Agent"
        $last = $null; $readErr = ''
        try {
            $openPr = Get-AgOpenPr $gh "pr/$Pkg"
            $mine = @(Get-AgReviewVerdicts (Get-AgThreadComments $gh $n $(if ($openPr) { "$($openPr.number)" } else { '' }))) | Where-Object { $_.Reviewer -eq $reviewer }
            $last = @($mine) | Select-Object -Last 1
        } catch { $readErr = "$_" }
        if ($readErr -or -not $last -or $last.Verdict -ne 'OK') {
            $m = if ($readErr) { "cannot read review comments ($readErr)" } elseif ($last) { "latest verdict is $($last.Verdict) at $($last.Sha)" } else { 'no review comment' }
            "REVIEW PRE-CHECK FAIL: $m from $reviewer on #$n. Expected: $(Format-AgReviewLine OK $reviewer '<sha>')"
            if (-not $DryRun) { exit 4 }
        } else { "review pre-check: $($last.Line) ($($last.Url))" }
        $holder = "$Agent/$Pkg"
        $lockSha = CreateRef 'lock/integration' (RepoRev origin/dev) $holder
        if (-not $lockSha) { 'LOCK HELD: another agent is merging; retry later'; exit 3 }
        $rc = 1
        $log = Join-Path $ctx.HandoffDir ("merge-{0}{1}-{2}.log" -f $(if ($DryRun) { 'dryrun-' } else { '' }), $Worktree, (Get-Date -Format 'yyyyMMdd-HHmm'))
        try {
            Labels $n @('st:merging') @('st:review')
            if (-not $DryRun) { $env:AG_INTEGRATION_LOCK = $Pkg }
            $t = @($Tests -split ',' | ForEach-Object { $_.Trim() } | Where-Object { $_ })
            $rm = @{ Worktree = $Worktree; Ticket = $Pkg; Issue = $n; Agent = $Agent; Owner = $owner; IssueTitle = $iss.title; Tests = $t
                ChecksTimeoutMin = $ChecksTimeoutMin; DryRun = [bool]$DryRun }
            $global:LASTEXITCODE = 1
            try {
                & "$sp\rebase-merge.ps1" @rm *>&1 | Tee-Object -FilePath $log | Select-Object -Last $(if ($DryRun) { 500 } else { 25 })
                $rc = $LASTEXITCODE
            } catch {
                "ERROR: $_" | Tee-Object -FilePath $log -Append; $rc = 1
            }
        } finally {
            Remove-Item Env:\AG_INTEGRATION_LOCK -ErrorAction SilentlyContinue
            DeleteRef 'lock/integration' $lockSha
        }
        $text = Get-Content $log -Encoding UTF8
        $tail = ($text | Select-Object -Last 15) -join "`n"
        # Evidence that must appear on the issue even when they scrolled out of the tail.
        $evidence = @($text | Where-Object { "$_" -match '^(checks: |go version: |go toolchain: |REVIEW OK|final review re-check: )' }) -join "`n"
        if ($evidence) { $tail = "$evidence`n...`n$tail" }
        $merged = $text | Select-String '^(MERGED via PR #(\d+)|DRY-RUN OK)' | Select-Object -Last 1
        if ($rc -eq 0 -and $merged) {
            $prNo = if ($merged.Matches[0].Groups[2].Success) { $merged.Matches[0].Groups[2].Value } else { '<new>' }
            DeleteRef "claim/$Pkg"
            Labels $n @('st:done') @('st:merging')
            Comment $n ("Đã gộp vào dev qua PR #${prNo}: $(git -C $repo rev-parse --short origin/dev).`n" + '```' + "`n$tail`n" + '```')
            Invoke-AgMut gh @('issue', 'close', "$n", '-R', $gh) | Out-Host
            "merged #$n $Pkg via PR #$prNo$(if ($DryRun) { ' (dry-run: nothing pushed, no labels changed)' })"
        } else {
            if ($rc -eq 0) { $rc = 1 }
            Labels $n @('st:review') @('st:merging')
            Comment $n ("Gộp thất bại (rc=$rc), log: $log`n" + '```' + "`n$tail`n" + '```')
            "MERGE FAIL rc=$rc #$n $Pkg (log $log)"; exit $rc
        }
    }
}
