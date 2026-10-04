package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Status là kết quả của một khối.
type Status string

const (
	Pass Status = "PASS"
	Fail Status = "FAIL"
	Skip Status = "SKIP"
)

// Result là kết quả chạy một khối.
type Result struct {
	Block  Block
	Status Status
	Detail string // lỗi PostgreSQL (Fail) hoặc lý do (Skip)
	ErrAt  int    // dòng trong tài liệu nơi lỗi xảy ra, 0 nếu không xác định
}

// Execute tạo database tạm ag_docsql_<hex> trên cụm của adminURL, chạy preamble
// rồi từng khối theo thứ tự trong MỘT giao dịch (mỗi khối một savepoint: khối
// lỗi được hoàn tác, các khối sau vẫn chạy), cuối cùng ROLLBACK và xóa database.
// Lỗi trả về là lỗi hạ tầng (kết nối, preamble), không phải lỗi của khối.
func Execute(ctx context.Context, adminURL, preamble string, blocks []Block, skip map[int]string) ([]Result, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	dbName := "ag_docsql_" + hex.EncodeToString(buf)

	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return nil, fmt.Errorf("kết nối quản trị: %w", err)
	}
	defer func() { _ = admin.Close(context.Background()) }()
	ident := pgx.Identifier{dbName}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+ident); err != nil {
		return nil, fmt.Errorf("tạo database %s: %w", dbName, err)
	}
	defer func() { _, _ = admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+ident+" WITH (FORCE)") }()

	u, err := url.Parse(adminURL)
	if err != nil {
		return nil, err
	}
	u.Path = "/" + dbName
	conn, err := pgx.Connect(ctx, u.String())
	if err != nil {
		return nil, fmt.Errorf("kết nối %s: %w", dbName, err)
	}
	defer func() { _ = conn.Close(context.Background()) }()
	pc := conn.PgConn()

	exec := func(sql string) error {
		_, err := pc.Exec(ctx, sql).ReadAll() // giao thức đơn giản: nhiều câu lệnh một lần
		return err
	}
	if err := exec("BEGIN"); err != nil {
		return nil, err
	}
	defer func() { _ = exec("ROLLBACK") }()
	if err := exec(preamble); err != nil {
		return nil, fmt.Errorf("preamble: %s", pgErrText(err))
	}

	results := make([]Result, 0, len(blocks))
	for _, b := range blocks {
		if r, ok := skip[b.Index]; ok {
			results = append(results, Result{Block: b, Status: Skip, Detail: r})
			continue
		}
		if err := exec("SAVEPOINT docsql_block"); err != nil {
			return results, err
		}
		if err := exec(b.SQL); err != nil {
			if rb := exec("ROLLBACK TO SAVEPOINT docsql_block"); rb != nil {
				return results, rb
			}
			results = append(results, Result{Block: b, Status: Fail, Detail: pgErrText(err), ErrAt: errLine(b, err)})
			continue
		}
		if err := exec("RELEASE SAVEPOINT docsql_block"); err != nil {
			return results, err
		}
		results = append(results, Result{Block: b, Status: Pass})
	}
	return results, nil
}

func pgErrText(err error) string {
	var pe *pgconn.PgError
	if !errors.As(err, &pe) {
		return err.Error()
	}
	s := fmt.Sprintf("%s %s: %s", pe.Severity, pe.Code, pe.Message)
	if pe.Detail != "" {
		s += " | DETAIL: " + pe.Detail
	}
	if pe.Hint != "" {
		s += " | HINT: " + pe.Hint
	}
	if pe.Where != "" {
		s += " | WHERE: " + pe.Where
	}
	return s
}

// errLine đổi vị trí lỗi (ký tự, đếm từ 1, trong chuỗi gửi đi) thành dòng tài liệu.
func errLine(b Block, err error) int {
	var pe *pgconn.PgError
	if !errors.As(err, &pe) || pe.Position <= 0 {
		return 0
	}
	r := []rune(b.SQL)
	p := int(pe.Position) - 1
	if p > len(r) {
		p = len(r)
	}
	return b.Line + strings.Count(string(r[:p]), "\n")
}
