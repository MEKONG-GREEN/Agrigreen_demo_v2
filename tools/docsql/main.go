// Lệnh docsql chạy DDL mẫu trong tài liệu kiến trúc trên PostgreSQL thật (R-OPS-04).
//
// Trích mọi khối ```sql của một tệp markdown theo thứ tự, tạo database tạm trên
// cụm của AG_TEST_PG_ADMIN_URL, chạy preamble.sql (extension, schema, vai mà tài
// liệu giả định) rồi từng khối; báo PASS/FAIL/SKIP mỗi khối kèm lỗi PostgreSQL.
// Khối minh họa hoặc lỗi tài liệu đã biết được bỏ qua qua skip.yaml (không sửa
// bản tài liệu gốc). Thoát 1 nếu có khối không bỏ qua mà lỗi hoặc skip.yaml có
// mục thừa; 2 nếu lỗi hạ tầng.
//
// Chạy: go run ./tools/docsql docs/kien-truc/AgriGreen-Thiet-ke-kien-truc-he-thong-v4_1.md
//
//	-list            chỉ liệt kê khối (không cần PostgreSQL)
//	-skip FILE       danh sách bỏ qua (mặc định tools/docsql/skip.yaml)
//	-preamble FILE   preamble khác bản nhúng sẵn
//	-admin URL       mặc định $AG_TEST_PG_ADMIN_URL
package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

//go:embed preamble.sql
var defaultPreamble string

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("docsql", flag.ContinueOnError)
	fs.SetOutput(stderr)
	admin := fs.String("admin", os.Getenv("AG_TEST_PG_ADMIN_URL"), "URL quản trị cụm PostgreSQL")
	skipPath := fs.String("skip", "tools/docsql/skip.yaml", "danh sách khối bỏ qua")
	preamblePath := fs.String("preamble", "", "preamble SQL (mặc định bản nhúng)")
	list := fs.Bool("list", false, "chỉ liệt kê khối")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		_, _ = fmt.Fprintln(stderr, "dùng: docsql [cờ] TỆP.md")
		return 2
	}
	doc := fs.Arg(0)
	src, err := os.ReadFile(doc)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}
	blocks := Extract(string(src))
	skips, err := LoadSkips(*skipPath)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 2
	}
	reasons, problems := Plan(doc, blocks, skips)

	if *list {
		for _, b := range blocks {
			mark := ""
			if r, ok := reasons[b.Index]; ok {
				mark = " [skip: " + r + "]"
			}
			_, _ = fmt.Fprintf(stdout, "#%d dòng %d hash %s%s\n", b.Index, b.Line, b.Hash(), mark)
		}
		return report(stdout, nil, problems)
	}
	if *admin == "" {
		_, _ = fmt.Fprintln(stderr, "docsql: cần AG_TEST_PG_ADMIN_URL hoặc -admin (Windows: . .\\scripts\\dev-env.ps1)")
		return 2
	}
	preamble := defaultPreamble
	if *preamblePath != "" {
		b, err := os.ReadFile(*preamblePath)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 2
		}
		preamble = string(b)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	results, err := Execute(ctx, *admin, preamble, blocks, reasons)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "docsql:", err)
		return 2
	}
	_, _ = fmt.Fprintf(stdout, "%s: %d khối sql\n", doc, len(blocks))
	return report(stdout, results, problems)
}

// report in kết quả từng khối và tổng; trả mã thoát.
func report(w io.Writer, results []Result, problems []string) int {
	count := map[Status]int{}
	for _, r := range results {
		count[r.Status]++
		line := fmt.Sprintf("%-4s #%d dòng %d hash %s", r.Status, r.Block.Index, r.Block.Line, r.Block.Hash())
		switch r.Status {
		case Fail:
			at := ""
			if r.ErrAt > 0 {
				at = fmt.Sprintf(" (tại dòng %d)", r.ErrAt)
			}
			line += at + "\n       " + r.Detail
		case Skip:
			line += " — " + r.Detail
		}
		_, _ = fmt.Fprintln(w, line)
	}
	for _, p := range problems {
		_, _ = fmt.Fprintln(w, p)
	}
	if results != nil {
		_, _ = fmt.Fprintf(w, "tổng: %d pass, %d skip, %d fail\n", count[Pass], count[Skip], count[Fail])
	}
	if count[Fail] > 0 || len(problems) > 0 {
		return 1
	}
	return 0
}
