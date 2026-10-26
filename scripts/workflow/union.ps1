param([Parameter(Mandatory)][string[]]$Files)
# Gỡ xung đột kiểu "giữ cả hai bên" (union) cho tệp tài liệu: bỏ các dòng đánh dấu, giữ nguyên nội dung hai phía.
$enc = New-Object System.Text.UTF8Encoding($false)
foreach ($f in $Files) {
    $text = [IO.File]::ReadAllText($f, $enc)
    $nl = if ($text.Contains("`r`n")) { "`r`n" } else { "`n" }
    $lines = $text -split "`r?`n"
    $out = $lines | Where-Object { $_ -notmatch '^(<<<<<<< |=======$|>>>>>>> |\|\|\|\|\|\|\| )' }
    [IO.File]::WriteAllText($f, ($out -join $nl), $enc)
    "union: $f ($($lines.Count - $out.Count) marker lines removed)"
}
