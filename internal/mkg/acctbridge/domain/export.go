package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"sort"
	"strconv"
	"strings"

	abport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/port"
)

// Định dạng tệp xuất (docs/dev/acctbridge-adapter.md). Tất định byte-cho-byte: chỉ phụ thuộc
// SummaryJournal (không thời điểm xuất, không người duyệt), UTF-8 KHÔNG BOM, xuống dòng CRLF (CSV) / LF
// (XML), số tiền 2 chữ số thập phân dấu chấm, không dấu phân cách hàng nghìn.
const (
	FormatCSV = "csv"
	FormatXML = "xml"

	// ExportSchemaVersion ghi trong tệp; đổi định dạng = phiên bản mới + bộ điều hợp mới.
	ExportSchemaVersion = "acctbridge.export.v1"
	// XMLNamespace của tệp XML.
	XMLNamespace = "urn:agrigreen:mkg:acctbridge:summary-journal:v1"
)

// CSVHeader là dòng tiêu đề của tệp CSV (dấu ;).
var CSVHeader = []string{
	"schema", "journal_id", "period_id", "period_code", "close_run_id", "currency", "source_hash", "mapping_version",
	"line_no", "account", "side", "debit", "credit", "category", "dimension", "description",
}

// FileStem là tên tệp tất định (không phần mở rộng): <period_code>_<journal_id>.
func FileStem(j abport.SummaryJournal) string { return j.PeriodCode + "_" + j.JournalID.String() }

func csvField(s string) string {
	if strings.ContainsAny(s, ";\"\r\n") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

// RenderCSV xuất bút toán dạng CSV (UTF-8, ';', CRLF, một dòng tiêu đề + một dòng mỗi dòng bút toán).
func RenderCSV(j abport.SummaryJournal) []byte {
	var b bytes.Buffer
	write := func(fields []string) {
		for i, f := range fields {
			if i > 0 {
				b.WriteByte(';')
			}
			b.WriteString(csvField(f))
		}
		b.WriteString("\r\n")
	}
	write(CSVHeader)
	for _, l := range j.Lines {
		debit, credit := "", ""
		if l.Side == abport.Debit {
			debit = l.Amount.String()
		} else {
			credit = l.Amount.String()
		}
		write([]string{
			ExportSchemaVersion, j.JournalID.String(), j.PeriodID.String(), j.PeriodCode, j.CloseRunID.String(), j.Currency, j.SourceHash,
			strconv.Itoa(j.MappingVersion), strconv.Itoa(l.LineNo), l.Account, string(l.Side), debit, credit, string(l.Category),
			l.Dimension, l.Description,
		})
	}
	return b.Bytes()
}

func xmlAttr(b *bytes.Buffer, name, value string) {
	b.WriteByte(' ')
	b.WriteString(name)
	b.WriteString(`="`)
	_ = xml.EscapeText(b, []byte(value)) // ghi vào bytes.Buffer không lỗi
	b.WriteByte('"')
}

// RenderXML xuất bút toán dạng XML (UTF-8, LF, thuộc tính theo thứ tự cố định, thụt 2 dấu cách).
func RenderXML(j abport.SummaryJournal) []byte {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString("<SummaryJournal")
	xmlAttr(&b, "xmlns", XMLNamespace)
	xmlAttr(&b, "schema", ExportSchemaVersion)
	xmlAttr(&b, "journalId", j.JournalID.String())
	xmlAttr(&b, "periodId", j.PeriodID.String())
	xmlAttr(&b, "periodCode", j.PeriodCode)
	xmlAttr(&b, "closeRunId", j.CloseRunID.String())
	xmlAttr(&b, "currency", j.Currency)
	xmlAttr(&b, "sourceHash", j.SourceHash)
	xmlAttr(&b, "mappingVersion", strconv.Itoa(j.MappingVersion))
	xmlAttr(&b, "totalDebit", j.TotalDebit.String())
	xmlAttr(&b, "totalCredit", j.TotalCredit.String())
	xmlAttr(&b, "lineCount", strconv.Itoa(len(j.Lines)))
	b.WriteString(">\n")
	for _, l := range j.Lines {
		b.WriteString("  <Line")
		xmlAttr(&b, "no", strconv.Itoa(l.LineNo))
		xmlAttr(&b, "account", l.Account)
		xmlAttr(&b, "side", string(l.Side))
		xmlAttr(&b, "amount", l.Amount.String())
		xmlAttr(&b, "category", string(l.Category))
		xmlAttr(&b, "dimension", l.Dimension)
		b.WriteString(">")
		_ = xml.EscapeText(&b, []byte(l.Description))
		b.WriteString("</Line>\n")
	}
	b.WriteString("</SummaryJournal>\n")
	return b.Bytes()
}

// Render xuất theo định dạng (csv | xml); ok = false nếu định dạng lạ.
func Render(j abport.SummaryJournal, format string) (body []byte, contentType string, ok bool) {
	switch format {
	case FormatCSV:
		return RenderCSV(j), "text/csv; charset=utf-8", true
	case FormatXML:
		return RenderXML(j), "application/xml; charset=utf-8", true
	}
	return nil, "", false
}

// SHA256Hex là băm hex thường của b.
func SHA256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// Manifest là tệp ký SHA-256 (định dạng sha256sum: "<hex>  <tên tệp>\n", sắp theo tên).
func Manifest(files map[string][]byte) []byte {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var b bytes.Buffer
	for _, n := range names {
		b.WriteString(SHA256Hex(files[n]))
		b.WriteString("  ")
		b.WriteString(n)
		b.WriteByte('\n')
	}
	return b.Bytes()
}
