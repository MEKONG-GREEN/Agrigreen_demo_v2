package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// SkipEntry là một khối được bỏ qua có lý do. Khớp theo tên tệp tài liệu (không
// kèm thư mục) cộng hash thân khối; Block (số thứ tự) chỉ để người đọc dễ tìm và
// được kiểm khớp. Đổi nội dung khối → hash đổi → mục bỏ qua thành "thừa" và tool
// hỏng, buộc người sửa tài liệu xem lại.
type SkipEntry struct {
	Doc    string `yaml:"doc"`
	Block  int    `yaml:"block"`
	Hash   string `yaml:"hash"`
	Reason string `yaml:"reason"`
}

type skipFile struct {
	Skips []SkipEntry `yaml:"skips"`
}

// LoadSkips đọc skip.yaml; tệp không tồn tại = danh sách rỗng.
func LoadSkips(path string) ([]SkipEntry, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseSkips(b)
}

// ParseSkips phân tích nội dung skip.yaml và kiểm từng mục đủ trường.
func ParseSkips(b []byte) ([]SkipEntry, error) {
	var f skipFile
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("skip.yaml: %w", err)
	}
	for i, s := range f.Skips {
		switch {
		case s.Doc == "":
			return nil, fmt.Errorf("skip.yaml mục %d: thiếu doc", i+1)
		case len(s.Hash) < 8:
			return nil, fmt.Errorf("skip.yaml mục %d: hash phải có ít nhất 8 ký tự hex", i+1)
		case strings.TrimSpace(s.Reason) == "":
			return nil, fmt.Errorf("skip.yaml mục %d: thiếu reason", i+1)
		}
	}
	return f.Skips, nil
}

// Plan ghép khối với mục bỏ qua của tài liệu doc. Trả lý do bỏ qua theo Index
// khối, và các vấn đề: mục thừa (không khớp khối nào) hoặc số thứ tự lệch.
func Plan(doc string, blocks []Block, skips []SkipEntry) (map[int]string, []string) {
	base := filepath.Base(doc)
	reasons := map[int]string{}
	var problems []string
	for _, s := range skips {
		if s.Doc != base {
			continue
		}
		matched := false
		for _, b := range blocks {
			if !strings.HasPrefix(b.Hash(), strings.ToLower(s.Hash)) {
				continue
			}
			matched = true
			reasons[b.Index] = s.Reason
			if s.Block != 0 && s.Block != b.Index {
				problems = append(problems, fmt.Sprintf("skip.yaml: hash %s là khối #%d, không phải #%d — sửa số thứ tự", s.Hash, b.Index, s.Block))
			}
		}
		if !matched {
			problems = append(problems, fmt.Sprintf("skip.yaml: mục thừa doc=%s block=%d hash=%s — khối đã đổi hoặc bị xóa, xem lại", s.Doc, s.Block, s.Hash))
		}
	}
	return reasons, problems
}
