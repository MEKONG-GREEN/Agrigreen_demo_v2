package app

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/docx"
	"github.com/google/uuid"
)

// copyWordData detaches the frozen vars, lists and individual rows. Binding
// verification must never fill placeholders in the immutable report snapshot.
func copyWordData(in doctpl.Data) doctpl.Data {
	out := doctpl.Data{}
	if in.Vars != nil {
		out.Vars = make(map[string]string, len(in.Vars))
		for key, value := range in.Vars {
			out.Vars[key] = value
		}
	}
	if in.Lists != nil {
		out.Lists = make(map[string][]doctpl.Item, len(in.Lists))
		for key, rows := range in.Lists {
			if rows == nil {
				out.Lists[key] = nil
				continue
			}
			copied := make([]doctpl.Item, len(rows))
			for i, row := range rows {
				if row != nil {
					copied[i] = make(doctpl.Item, len(row))
					for column, value := range row {
						copied[i][column] = value
					}
				}
			}
			out.Lists[key] = copied
		}
	}
	return out
}

// verifyWordBinding uses only public Numbering/DateLine contracts and Data.Hash.
// It neither renders a document nor allocates a number. The caller supplies the
// pinned owner template hash; subject/tenant authorization precedes this check.
func verifyWordBinding(req exportport.Request, templateHash string, run exportport.Run) error {
	invalid := func() error {
		return ErrDependency.WithDetail("Word đã lưu không khớp bản xuất hoặc thiếu bằng chứng liên kết")
	}
	if run.ID == uuid.Nil || run.Seq < 1 || run.ActorRef == uuid.Nil || run.At.IsZero() || run.Subject != req.Subject || run.Template != req.Template || req.Version < 1 || run.TemplateVersion != req.Version || len(templateHash) != 64 || run.TemplateHash != templateHash || !exportport.ValidSubject(run.Subject) || !exportport.ValidFileName(run.FileName) || run.FileName != req.FileName || run.ContentType != docx.ContentType || run.Size <= 0 || run.Size != int64(len(run.Body)) {
		return invalid()
	}
	sum := sha256.Sum256(run.Body)
	if run.BodyHash != hex.EncodeToString(sum[:]) {
		return invalid()
	}
	data := copyWordData(req.Data)
	if data.Vars == nil {
		return invalid()
	}
	if number := req.Number; number != nil {
		if number.Var == "" || !exportport.ValidSeries(number.Series) {
			return invalid()
		}
		if supplied := strings.TrimSpace(data.Vars[number.Var]); supplied != "" {
			if run.DocNumberAuto || run.DocNumber != supplied {
				return invalid()
			}
		} else {
			if !run.DocNumberAuto || !validArchiveAutoNumber(run.DocNumber, number.Series) {
				return invalid()
			}
			data.Vars[number.Var] = run.DocNumber
		}
	} else if run.DocNumberAuto || run.DocNumber != "" {
		return invalid()
	}
	if line := req.Date; line != nil {
		if line.Var == "" {
			return invalid()
		}
		if strings.TrimSpace(data.Vars[line.Var]) == "" {
			data.Vars[line.Var] = doctpl.PlaceDateLine(line.Place, run.At)
		}
	}
	if run.InputHash != data.Hash() {
		return invalid()
	}
	return nil
}

func archiveBodyMatches(body []byte, expected string) bool {
	if len(body) == 0 || len(expected) != 64 {
		return false
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]) == expected
}

// Numbering's public contract is canonical positive {n}/<Series>, with n stored
// in PostgreSQL bigint. Bound length and ASCII digits before integer conversion.
func validArchiveAutoNumber(value, series string) bool {
	counter, ok := strings.CutSuffix(value, "/"+series)
	if !ok || len(counter) < 1 || len(counter) > 19 || counter[0] == '0' {
		return false
	}
	for _, digit := range counter {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	n, err := strconv.ParseInt(counter, 10, 64)
	return err == nil && n > 0
}
