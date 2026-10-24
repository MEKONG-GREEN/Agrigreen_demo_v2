package store

// SQL của điều tra giá thành (W11.78, pf 20261024091116): đợt, xã điều tra, cán bộ điều tra, phiếu, lần nhập + dòng lỗi,
// bảng từ đồng nghĩa. Sổ chỉ ghi thêm — phiên bản hiện hành = seq lớn nhất của chuỗi. Số qua text ↔ decimal (R-CODE-01).

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
)

// LockSurveyRound khóa cố vấn theo đợt — mọi thao tác ghi của một đợt (đổi trạng thái, nhập tệp, rà soát) chạy tuần tự.
func LockSurveyRound(ctx context.Context, tx pgx.Tx, tid, round uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_price.survey_round:' || $1::text || ':' || $2::text, 0))`,
		tid, round)
	return err
}

// LockSurveySynonyms khóa cố vấn bảng từ đồng nghĩa của tenant.
func LockSurveySynonyms(ctx context.Context, tx pgx.Tx, tid uuid.UUID) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pf_price.survey_synonym:' || $1::text, 0))`, tid)
	return err
}

// SurveyRound là một phiên bản đợt điều tra.
type SurveyRound struct {
	RoundID      uuid.UUID
	Seq          int64
	Profile      string
	Name         string
	Season       string
	Year         int
	CollectFrom  time.Time
	CollectTo    time.Time
	Crops        []string
	Note         string
	Status       string
	Op           string
	Reason       string
	EventTime    time.Time
	Actor        uuid.UUID
	CreatedAt    time.Time // event_time của phiên bản đầu (chỉ khi đọc danh sách)
	CreatedActor uuid.UUID
}

// InsertSurveyRound ghi một phiên bản đợt; trả seq.
func InsertSurveyRound(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, r SurveyRound, at time.Time, actor uuid.UUID, idem string) (int64, error) {
	var seq int64
	err := tx.QueryRow(ctx, `INSERT INTO pf_price.survey_round (id, tenant_id, stream_key, round_id, profile, name, season, year, collect_from,
	    collect_to, crops, note, status, op, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'sround:' || $3::uuid::text, $3::uuid, $4, $5, $6, $7, $8::date, $9::date, $10::text[], $11, $12, $13, $14, $15, $16, $17, $18)
	  RETURNING seq`,
		id, tid, r.RoundID, r.Profile, r.Name, r.Season, r.Year, dateOnly(r.CollectFrom), dateOnly(r.CollectTo), r.Crops, nullText(r.Note),
		r.Status, r.Op, nullText(r.Reason), at.UTC(), actor, idem, domain.RuleVersion).Scan(&seq)
	return seq, err
}

const roundCols = `round_id, seq, profile, name, season, year, collect_from, collect_to, crops, coalesce(note, ''), status, op,
  coalesce(reason, ''), event_time, actor_ref`

func scanRound(r pgx.CollectableRow) (SurveyRound, error) {
	var x SurveyRound
	err := r.Scan(&x.RoundID, &x.Seq, &x.Profile, &x.Name, &x.Season, &x.Year, &x.CollectFrom, &x.CollectTo, &x.Crops, &x.Note, &x.Status,
		&x.Op, &x.Reason, &x.EventTime, &x.Actor)
	return x, err
}

// SurveyRounds đọc phiên bản hiện hành của mọi đợt của profile, mới tạo trước.
func SurveyRounds(ctx context.Context, tx pgx.Tx, profile string) ([]SurveyRound, error) {
	rows, err := tx.Query(ctx, `SELECT `+roundCols+` FROM (SELECT DISTINCT ON (round_id) * FROM pf_price.survey_round
	    WHERE profile = $1 ORDER BY round_id, seq DESC) v ORDER BY round_id DESC`, profile)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanRound)
}

// SurveyRoundHistory đọc mọi phiên bản của đợt (nhật ký), theo seq.
func SurveyRoundHistory(ctx context.Context, tx pgx.Tx, round uuid.UUID) ([]SurveyRound, error) {
	rows, err := tx.Query(ctx, `SELECT `+roundCols+` FROM pf_price.survey_round WHERE round_id = $1 ORDER BY seq`, round)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanRound)
}

// SurveyCommune là xã điều tra + chỉ tiêu theo nhóm cây.
type SurveyCommune struct {
	Commune string
	Targets map[string]int // nhóm cây → chỉ tiêu
	Active  bool
}

// InsertSurveyCommune ghi một phiên bản xã điều tra.
func InsertSurveyCommune(ctx context.Context, tx pgx.Tx, id, tid, round uuid.UUID, c SurveyCommune, at time.Time, actor uuid.UUID, idem string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_price.survey_commune (id, tenant_id, stream_key, round_id, commune_code, target_lua, target_rau_mau,
	    target_nam, target_cay_an_trai, active, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'scommune:' || $3::uuid::text || ':' || $4::text, $3::uuid, $4::text, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		id, tid, round, c.Commune, c.Targets[domain.CropRice], c.Targets[domain.CropVeg], c.Targets[domain.CropMush],
		c.Targets[domain.CropFruit], c.Active, at.UTC(), actor, idem, domain.RuleVersion)
	return err
}

// SurveyCommunes đọc phiên bản hiện hành của xã điều tra của các đợt (kể cả đã bỏ — Active = false), theo mã.
func SurveyCommunes(ctx context.Context, tx pgx.Tx, rounds []uuid.UUID) (map[uuid.UUID][]SurveyCommune, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (round_id, commune_code) round_id, commune_code, target_lua, target_rau_mau, target_nam,
	    target_cay_an_trai, active FROM pf_price.survey_commune WHERE round_id = ANY($1::uuid[]) ORDER BY round_id, commune_code, seq DESC`, uuidStrs(rounds))
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID][]SurveyCommune{}
	_, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (struct{}, error) {
		var (
			rid        uuid.UUID
			c          SurveyCommune
			l, v, m, f int
		)
		if err := r.Scan(&rid, &c.Commune, &l, &v, &m, &f, &c.Active); err != nil {
			return struct{}{}, err
		}
		c.Targets = map[string]int{domain.CropRice: l, domain.CropVeg: v, domain.CropMush: m, domain.CropFruit: f}
		out[rid] = append(out[rid], c)
		return struct{}{}, nil
	})
	return out, err
}

// SurveyOfficer là cán bộ điều tra (định danh ở kho — SubjectRef).
type SurveyOfficer struct {
	OfficerID  uuid.UUID
	Commune    string
	SubjectRef uuid.UUID
	UnitName   string
	Active     bool
}

// InsertSurveyOfficer ghi một phiên bản cán bộ điều tra.
func InsertSurveyOfficer(ctx context.Context, tx pgx.Tx, id, tid, round uuid.UUID, o SurveyOfficer, at time.Time, actor uuid.UUID, idem string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_price.survey_officer (id, tenant_id, stream_key, round_id, officer_id, commune_code, subject_ref,
	    unit_name, active, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'sofficer:' || $3::uuid::text || ':' || $4::uuid::text, $3::uuid, $4::uuid, $5, $6, $7, $8, $9, $10, $11, $12)`,
		id, tid, round, o.OfficerID, o.Commune, o.SubjectRef, o.UnitName, o.Active, at.UTC(), actor, idem, domain.RuleVersion)
	return err
}

// SurveyOfficers đọc phiên bản hiện hành của cán bộ điều tra của đợt (kể cả đã bỏ).
func SurveyOfficers(ctx context.Context, tx pgx.Tx, round uuid.UUID) ([]SurveyOfficer, error) {
	rows, err := tx.Query(ctx, `SELECT officer_id, commune_code, subject_ref, unit_name, active FROM (SELECT DISTINCT ON (officer_id) *
	    FROM pf_price.survey_officer WHERE round_id = $1 ORDER BY officer_id, seq DESC) v ORDER BY commune_code, officer_id`, round)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (SurveyOfficer, error) {
		var o SurveyOfficer
		err := r.Scan(&o.OfficerID, &o.Commune, &o.SubjectRef, &o.UnitName, &o.Active)
		return o, err
	})
}

// SurveyForm là một phiên bản phiếu điều tra.
type SurveyForm struct {
	FormID        uuid.UUID
	Seq           int64
	RoundID       uuid.UUID
	ImportID      uuid.UUID
	RowNo         int
	Commune       string
	Crop          string
	Item          string
	FarmerRef     uuid.UUID
	HasPhone      bool
	OfficerID     *uuid.UUID
	AreaHa        decimal.Decimal
	OutputKg      decimal.Decimal
	SalePrice     decimal.Decimal
	OtherIncome   decimal.Decimal
	Costs         map[string]decimal.Decimal
	Status        string
	Op            string
	Note          string
	VerifiedPrice *decimal.Decimal
	Unverifiable  bool
	EventTime     time.Time
	Actor         uuid.UUID
}

// InsertSurveyForm ghi một phiên bản phiếu.
func InsertSurveyForm(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, f SurveyForm, at time.Time, actor uuid.UUID, idem string) error {
	var vp any
	if f.VerifiedPrice != nil {
		vp = f.VerifiedPrice.String()
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_price.survey_form (id, tenant_id, stream_key, form_id, round_id, import_id, row_no, commune_code,
	    crop, item_code, farmer_ref, has_phone, officer_id, area_ha, output_kg, sale_price, other_income, cost_seed, cost_fertilizer,
	    cost_pesticide, cost_labor, cost_machine, cost_other, status, op, note, verified_price, unverifiable, event_time, actor_ref,
	    idem_key, rule_version)
	  VALUES ($1, $2, 'sform:' || $3::uuid::text, $3::uuid, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::numeric, $14::numeric, $15::numeric,
	    $16::numeric, $17::numeric, $18::numeric, $19::numeric, $20::numeric, $21::numeric, $22::numeric, $23, $24, $25, $26::numeric,
	    $27, $28, $29, $30, $31)`,
		id, tid, f.FormID, f.RoundID, f.ImportID, f.RowNo, f.Commune, f.Crop, f.Item, f.FarmerRef, f.HasPhone, f.OfficerID,
		f.AreaHa.String(), f.OutputKg.String(), f.SalePrice.String(), f.OtherIncome.String(), f.Costs["seed"].String(),
		f.Costs["fertilizer"].String(), f.Costs["pesticide"].String(), f.Costs["labor"].String(), f.Costs["machine"].String(),
		f.Costs["other"].String(), f.Status, f.Op, nullText(f.Note), vp, f.Unverifiable, at.UTC(), actor, idem, domain.RuleVersion)
	return err
}

const formCols = `form_id, seq, round_id, import_id, row_no, commune_code, crop, item_code, farmer_ref, has_phone, officer_id,
  area_ha::text, output_kg::text, sale_price::text, other_income::text, cost_seed::text, cost_fertilizer::text, cost_pesticide::text,
  cost_labor::text, cost_machine::text, cost_other::text, status, op, coalesce(note, ''), verified_price::text, unverifiable, event_time,
  actor_ref`

func scanForm(r pgx.CollectableRow) (SurveyForm, error) {
	var (
		f  SurveyForm
		ns [10]string
		vp *string
	)
	if err := r.Scan(&f.FormID, &f.Seq, &f.RoundID, &f.ImportID, &f.RowNo, &f.Commune, &f.Crop, &f.Item, &f.FarmerRef, &f.HasPhone,
		&f.OfficerID, &ns[0], &ns[1], &ns[2], &ns[3], &ns[4], &ns[5], &ns[6], &ns[7], &ns[8], &ns[9], &f.Status, &f.Op, &f.Note, &vp,
		&f.Unverifiable, &f.EventTime, &f.Actor); err != nil {
		return f, err
	}
	ds, err := decs(ns[:]...)
	if err != nil {
		return f, err
	}
	f.AreaHa, f.OutputKg, f.SalePrice, f.OtherIncome = ds[0], ds[1], ds[2], ds[3]
	f.Costs = map[string]decimal.Decimal{}
	for i, k := range domain.CostKeys {
		f.Costs[k] = ds[4+i]
	}
	if vp != nil {
		d, err := decimal.NewFromString(*vp)
		if err != nil {
			return f, err
		}
		f.VerifiedPrice = &d
	}
	return f, nil
}

// SurveyForms đọc phiên bản hiện hành của mọi phiếu của các đợt (kể cả Đã xóa), theo đợt rồi thứ tự nhập.
func SurveyForms(ctx context.Context, tx pgx.Tx, rounds []uuid.UUID) ([]SurveyForm, error) {
	rows, err := tx.Query(ctx, `SELECT `+formCols+` FROM (SELECT DISTINCT ON (form_id) * FROM pf_price.survey_form
	    WHERE round_id = ANY($1::uuid[]) ORDER BY form_id, seq DESC) v ORDER BY round_id, form_id`, uuidStrs(rounds))
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanForm)
}

// SurveyFormHistory đọc mọi phiên bản của phiếu (nhật ký — BR-05 FN-TI-02.12).
func SurveyFormHistory(ctx context.Context, tx pgx.Tx, form uuid.UUID) ([]SurveyForm, error) {
	rows, err := tx.Query(ctx, `SELECT `+formCols+` FROM pf_price.survey_form WHERE form_id = $1 ORDER BY seq`, form)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanForm)
}

// SurveyImport là một lần nhập tệp.
type SurveyImport struct {
	ImportID  uuid.UUID
	RoundID   uuid.UUID
	SHA256    string
	FileName  string
	Rows      int
	Created   int
	Skipped   int
	Warnings  int
	Errors    int
	EventTime time.Time
	Actor     uuid.UUID
}

// InsertSurveyImport ghi lần nhập.
func InsertSurveyImport(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, im SurveyImport, at time.Time, actor uuid.UUID, idem string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_price.survey_import (id, tenant_id, stream_key, import_id, round_id, file_sha256, file_name,
	    rows_total, created, skipped, warnings, errors, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'simport:' || $3::uuid::text, $3::uuid, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)`,
		id, tid, im.ImportID, im.RoundID, im.SHA256, im.FileName, im.Rows, im.Created, im.Skipped, im.Warnings, im.Errors, at.UTC(), actor,
		idem, domain.RuleVersion)
	return err
}

// SurveyImports đọc các lần nhập của đợt, mới nhất trước.
func SurveyImports(ctx context.Context, tx pgx.Tx, round uuid.UUID) ([]SurveyImport, error) {
	rows, err := tx.Query(ctx, `SELECT import_id, round_id, file_sha256, file_name, rows_total, created, skipped, warnings, errors,
	    event_time, actor_ref FROM pf_price.survey_import WHERE round_id = $1 ORDER BY event_time DESC, import_id DESC`, round)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (SurveyImport, error) {
		var x SurveyImport
		err := r.Scan(&x.ImportID, &x.RoundID, &x.SHA256, &x.FileName, &x.Rows, &x.Created, &x.Skipped, &x.Warnings, &x.Errors,
			&x.EventTime, &x.Actor)
		return x, err
	})
}

// SurveyIssue là dòng lỗi / cảnh báo của lần nhập.
type SurveyIssue struct {
	RowNo   int
	Level   string // error | warning
	Message string
}

// InsertSurveyIssue ghi dòng lỗi / cảnh báo.
func InsertSurveyIssue(ctx context.Context, tx pgx.Tx, id, tid, imp uuid.UUID, is SurveyIssue, at time.Time, actor uuid.UUID, idem string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_price.survey_issue (id, tenant_id, stream_key, import_id, row_no, level, message, event_time,
	    actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'sissue:' || $3::uuid::text || ':' || $4::int::text, $3::uuid, $4::int, $5, $6, $7, $8, $9, $10)`,
		id, tid, imp, is.RowNo, is.Level, is.Message, at.UTC(), actor, idem, domain.RuleVersion)
	return err
}

// SurveyIssues đọc dòng lỗi / cảnh báo của lần nhập, theo số dòng.
func SurveyIssues(ctx context.Context, tx pgx.Tx, imp uuid.UUID) ([]SurveyIssue, error) {
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (row_no) row_no, level, message FROM pf_price.survey_issue WHERE import_id = $1
	  ORDER BY row_no, seq DESC`, imp)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (SurveyIssue, error) {
		var x SurveyIssue
		err := r.Scan(&x.RowNo, &x.Level, &x.Message)
		return x, err
	})
}

// InsertSurveySynonym ghi một phiên bản từ đồng nghĩa.
func InsertSurveySynonym(ctx context.Context, tx pgx.Tx, id, tid uuid.UUID, kind, key, target string, active bool, at time.Time,
	actor uuid.UUID, idem string) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_price.survey_synonym (id, tenant_id, stream_key, kind, raw_key, target_code, active, event_time,
	    actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, 'ssyn:' || $3::text || ':' || $4::text, $3::text, $4::text, $5, $6, $7, $8, $9, $10)`,
		id, tid, kind, key, target, active, at.UTC(), actor, idem, domain.RuleVersion)
	return err
}

// SurveySynonyms đọc bảng từ đồng nghĩa đang dùng của tenant: kind → khóa chuẩn hóa → mã.
func SurveySynonyms(ctx context.Context, tx pgx.Tx) (map[string]map[string]string, error) {
	rows, err := tx.Query(ctx, `SELECT kind, raw_key, target_code FROM (SELECT DISTINCT ON (kind, raw_key) kind, raw_key, target_code, active
	    FROM pf_price.survey_synonym ORDER BY kind, raw_key, seq DESC) v WHERE active`)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]string{"item": {}, "commune": {}}
	_, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (struct{}, error) {
		var k, key, code string
		if err := r.Scan(&k, &key, &code); err != nil {
			return struct{}{}, err
		}
		if out[k] == nil {
			out[k] = map[string]string{}
		}
		out[k][key] = code
		return struct{}{}, nil
	})
	return out, err
}

func uuidStrs(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}
