package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Block là một khối ```sql trong tài liệu markdown.
type Block struct {
	Index int    // thứ tự trong các khối sql, đếm từ 1
	Line  int    // dòng của dòng đầu tiên trong thân khối (sau hàng rào mở), đếm từ 1
	SQL   string // thân khối, giữ nguyên, mỗi dòng kết thúc bằng \n
}

// Hash trả 12 ký tự hex đầu của SHA-256 thân khối — dùng làm khóa trong skip.yaml.
func (b Block) Hash() string {
	s := sha256.Sum256([]byte(b.SQL))
	return hex.EncodeToString(s[:])[:12]
}

// Extract trích mọi khối mã có ngôn ngữ "sql" theo thứ tự xuất hiện.
// Theo CommonMark: hàng rào ``` hoặc ~~~ (≥ 3 ký tự, thụt ≤ 3 dấu cách), đóng bằng
// cùng ký tự với độ dài ≥ hàng rào mở. Khối ```sql nằm bên trong một khối mã khác
// (vd ````markdown) không được tính. Khối chưa đóng kéo đến hết tệp.
func Extract(md string) []Block {
	var (
		out      []Block
		inFence  bool
		fenceCh  byte
		fenceLen int
		isSQL    bool
		start    int
		body     strings.Builder
	)
	sc := bufio.NewScanner(strings.NewReader(md))
	sc.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	n := 0
	flush := func() {
		if isSQL {
			out = append(out, Block{Index: len(out) + 1, Line: start, SQL: body.String()})
		}
		body.Reset()
	}
	for sc.Scan() {
		n++
		line := strings.TrimSuffix(sc.Text(), "\r")
		ch, l, info, ok := fence(line)
		if !inFence {
			if ok {
				inFence, fenceCh, fenceLen = true, ch, l
				lang, _, _ := strings.Cut(strings.TrimSpace(info), " ")
				isSQL = strings.EqualFold(lang, "sql")
				start = n + 1
			}
			continue
		}
		if ok && ch == fenceCh && l >= fenceLen && strings.TrimSpace(info) == "" {
			flush()
			inFence = false
			continue
		}
		body.WriteString(line)
		body.WriteByte('\n')
	}
	if inFence {
		flush()
	}
	return out
}

// fence nhận diện một dòng hàng rào mã: ký tự, độ dài, phần thông tin sau hàng rào.
func fence(line string) (ch byte, n int, info string, ok bool) {
	i := 0
	for i < len(line) && i < 4 && line[i] == ' ' {
		i++
	}
	if i > 3 || i >= len(line) || (line[i] != '`' && line[i] != '~') {
		return 0, 0, "", false
	}
	ch = line[i]
	j := i
	for j < len(line) && line[j] == ch {
		j++
	}
	if j-i < 3 {
		return 0, 0, "", false
	}
	info = line[j:]
	if ch == '`' && strings.ContainsRune(info, '`') {
		return 0, 0, "", false
	}
	return ch, j - i, info, true
}
