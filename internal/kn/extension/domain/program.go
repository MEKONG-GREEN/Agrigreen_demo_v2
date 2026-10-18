package domain

import (
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ProgramReport là một phiên bản báo cáo tiến độ Đề án 1 triệu ha (FR-03) của
// một HTX trong một vụ: diện tích đăng ký / đang triển khai / hoàn thành.
type ProgramReport struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	VersionKey       string
	CommuneCode      string
	ProvinceCode     string
	CooperativeRef   uuid.UUID
	SeasonCode       string
	SeasonYear       int
	RegisteredHa     decimal.Decimal
	DeployingHa      decimal.Decimal
	CompletedHa      decimal.Decimal
	Note             string
	Version          int
	PreviousID       uuid.UUID
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	EventTime        time.Time
	RecordedAt       time.Time
	ActorRef         uuid.UUID
	DeviceID         *string
	IdemKey          string
	Seq              int64
}

// ProgramVersionKey: mọi lần gửi của cùng HTX × vụ là phiên bản của cùng một số liệu.
func (p ProgramReport) ProgramVersionKey() string {
	return strings.Join([]string{"program", p.CommuneCode, p.CooperativeRef.String(), p.SeasonCode, strconv.Itoa(p.SeasonYear)}, ":")
}

// ProgramStreamKey là stream của sổ program_report.
func ProgramStreamKey(versionKey string) string { return "program:" + versionKey }

// Validate kiểm giá trị (danh mục vụ, HTX tra ở app/).
func (p *ProgramReport) Validate() error {
	p.CommuneCode = strings.TrimSpace(p.CommuneCode)
	p.SeasonCode = strings.TrimSpace(p.SeasonCode)
	p.Note = strings.TrimSpace(p.Note)
	switch {
	case p.CommuneCode == "":
		return fieldErr("commune_code", "bắt buộc")
	case p.CooperativeRef == uuid.Nil:
		return fieldErr("cooperative_ref", "bắt buộc — tiến độ Đề án theo HTX (FR-03)")
	case !codeOK(p.SeasonCode):
		return fieldErr("season_code", "bắt buộc, mã vụ theo danh mục")
	case p.SeasonYear < 2000 || p.SeasonYear > 2100:
		return fieldErr("season_year", "năm vụ không hợp lệ")
	case len([]rune(p.Note)) > 2000:
		return fieldErr("note", "tối đa 2000 ký tự")
	case ContainsPII(p.Note):
		return FieldError{Field: "note", Err: ErrPIIInText}
	}
	for _, f := range []struct {
		name string
		v    decimal.Decimal
	}{{"registered_area_ha", p.RegisteredHa}, {"deploying_area_ha", p.DeployingHa}, {"completed_area_ha", p.CompletedHa}} {
		v := f.v
		if err := areaOK(f.name, &v, false); err != nil {
			return err
		}
	}
	if p.DeployingHa.Add(p.CompletedHa).GreaterThan(p.RegisteredHa) {
		return fieldErr("completed_area_ha", "đang triển khai + hoàn thành không vượt diện tích đăng ký")
	}
	return nil
}
