package store

// Báo cáo Chi cục KN v7.7 (migration pf 000072). Mọi hàm nhận pgx.Tx đã ở ngữ cảnh tenant (RLS).

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DispatchRuleVersion là rule_version của sổ báo cáo Chi cục.
const DispatchRuleVersion = "reports.dispatch.v1"

// Recipient là một dòng danh bạ người nhận Chi cục (hộp thư đơn vị — Q-09 tạm).
type DispatchRecipient struct {
	ID         uuid.UUID
	Department string
	Province   string
	Label      string
	Address    *string `pii:"true"`
	ContactRef *uuid.UUID
	Kind       string
	IsDefault  bool
	Active     bool
	LastUsedAt *time.Time
	CreatedBy  uuid.UUID
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

const recCols = `id, department_code, province_code, label, mailbox, contact_ref, kind, is_default, active, last_used_at, created_by, created_at, updated_at`

func scanRec(r pgx.Row) (DispatchRecipient, error) {
	var x DispatchRecipient
	err := r.Scan(&x.ID, &x.Department, &x.Province, &x.Label, &x.Address, &x.ContactRef, &x.Kind, &x.IsDefault, &x.Active, &x.LastUsedAt, &x.CreatedBy,
		&x.CreatedAt, &x.UpdatedAt)
	return x, err
}

// InsertRecipient ghi người nhận mới.
func InsertRecipient(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r DispatchRecipient) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_reports.dispatch_recipient (tenant_id, id, department_code, province_code, label, mailbox, contact_ref, kind,
	    is_default, active, created_by, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, true, $10, $11, $11)`,
		tid, r.ID, r.Department, r.Province, r.Label, r.Address, r.ContactRef, r.Kind, r.IsDefault, r.CreatedBy, r.CreatedAt.UTC())
	return err
}

// Recipients trả danh bạ của tỉnh × phòng ban.
func Recipients(ctx context.Context, tx pgx.Tx, province, dept string, activeOnly bool) ([]DispatchRecipient, error) {
	rows, err := tx.Query(ctx, "SELECT "+recCols+` FROM pf_reports.dispatch_recipient WHERE province_code = $1 AND department_code = $2
	  AND (NOT $3::boolean OR active) ORDER BY kind DESC, label`, province, dept, activeOnly)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (DispatchRecipient, error) { return scanRec(r) })
}

// RecipientByID đọc (khóa nếu lock) một người nhận; không có → nil.
func RecipientByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, lock bool) (*DispatchRecipient, error) {
	q := "SELECT " + recCols + " FROM pf_reports.dispatch_recipient WHERE id = $1::uuid"
	if lock {
		q += " FOR UPDATE"
	}
	x, err := scanRec(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &x, nil
}

// UpdateRecipient ghi trạng thái hiện hành.
func UpdateRecipient(ctx context.Context, tx pgx.Tx, r DispatchRecipient) error {
	_, err := tx.Exec(ctx, `UPDATE pf_reports.dispatch_recipient SET label = $2, kind = $3, is_default = $4, active = $5, last_used_at = $6, updated_at = $7
	  WHERE id = $1`, r.ID, r.Label, r.Kind, r.IsDefault, r.Active, r.LastUsedAt, r.UpdatedAt.UTC())
	return err
}

// RecipientEvent là một dòng sổ danh bạ.
type RecipientEvent struct {
	ID          uuid.UUID
	RecipientID uuid.UUID
	Action      string
	Department  string
	Province    string
	Before      json.RawMessage
	After       json.RawMessage
	Reason      *string
	EventTime   time.Time
	ActorRef    uuid.UUID
	IdemKey     string
}

// InsertRecipientEvent ghi sổ danh bạ (ảnh chụp không chứa địa chỉ — chỉ nhãn, loại, trạng thái).
func InsertRecipientEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e RecipientEvent) error {
	var before *string
	if len(e.Before) > 0 {
		s := string(e.Before)
		before = &s
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_reports.dispatch_recipient_event (id, tenant_id, stream_key, recipient_id, action, department_code,
	    province_code, before, after, reason, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10, $11, $12, $13, $14)`,
		e.ID, tid, "recipient:"+e.RecipientID.String(), e.RecipientID, e.Action, e.Department, e.Province, before, string(e.After), e.Reason,
		e.EventTime.UTC(), e.ActorRef, e.IdemKey, DispatchRuleVersion)
	return err
}

// Dispatch là một dòng pf_reports.dispatch.
type Dispatch struct {
	ID          uuid.UUID
	Department  string
	Province    string
	PeriodID    uuid.UUID
	PeriodLabel string
	RunIDs      []uuid.UUID
	Forms       []string
	Formats     []string
	Provisional bool
	Missing     []string
	Status      string
	Recipients  []uuid.UUID
	CC          []uuid.UUID
	Subject     *string
	Body        *string
	LinkOnly    bool
	Attempts    int
	LastError   *string
	SentAt      *time.Time
	CreatedBy   uuid.UUID
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// ClaimUntil (P0-WORKER-REPORTS): hạn nhận việc của worker đang gửi; nil = không ai giữ.
	ClaimUntil *time.Time
}

const dispCols = `id, department_code, province_code, period_id, period_label, run_ids, forms, formats, provisional, missing_communes, status,
  recipient_ids, cc_ids, subject, body, link_only, attempts, last_error, sent_at, created_by, created_at, updated_at, claim_until`

func scanDisp(r pgx.Row) (Dispatch, error) {
	var d Dispatch
	err := r.Scan(&d.ID, &d.Department, &d.Province, &d.PeriodID, &d.PeriodLabel, &d.RunIDs, &d.Forms, &d.Formats, &d.Provisional, &d.Missing, &d.Status,
		&d.Recipients, &d.CC, &d.Subject, &d.Body, &d.LinkOnly, &d.Attempts, &d.LastError, &d.SentAt, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt, &d.ClaimUntil)
	return d, err
}

// InsertDispatch ghi lần xuất.
func InsertDispatch(ctx context.Context, tx pgx.Tx, tid uuid.UUID, d Dispatch) error {
	if d.Missing == nil {
		d.Missing = []string{}
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_reports.dispatch (tenant_id, id, department_code, province_code, period_id, period_label, run_ids, forms,
	    formats, provisional, missing_communes, status, created_by, created_at, updated_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7::uuid[], $8::text[], $9::text[], $10, $11::text[], $12, $13, $14, $14)`,
		tid, d.ID, d.Department, d.Province, d.PeriodID, d.PeriodLabel, idStrs(d.RunIDs), d.Forms, d.Formats, d.Provisional, d.Missing, d.Status, d.CreatedBy,
		d.CreatedAt.UTC())
	return err
}

// DispatchByID đọc (khóa nếu lock) một lần gửi; không có → nil. Khóa FOR NO KEY UPDATE: vẫn tuần tự hóa người cập
// nhật lần gửi, nhưng không chặn giao dịch khác chèn dòng có khóa ngoại tới nó (KEY SHARE) — HandleDispatchRequested
// giữ khóa này trong giao dịch inbox trong khi ghi liên kết tải ở giao dịch riêng.
func DispatchByID(ctx context.Context, tx pgx.Tx, id uuid.UUID, lock bool) (*Dispatch, error) {
	q := "SELECT " + dispCols + " FROM pf_reports.dispatch WHERE id = $1::uuid"
	if lock {
		q += " FOR NO KEY UPDATE"
	}
	d, err := scanDisp(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Dispatches trả lịch sử gửi của tỉnh × phòng ban (mới nhất trước).
func Dispatches(ctx context.Context, tx pgx.Tx, province, dept string, periodID *uuid.UUID, before *uuid.UUID, limit int) ([]Dispatch, error) {
	rows, err := tx.Query(ctx, "SELECT "+dispCols+` FROM pf_reports.dispatch
	  WHERE ($1 = '' OR province_code = $1) AND ($2 = '' OR department_code = $2) AND ($3::uuid IS NULL OR period_id = $3::uuid)
	    AND ($4::uuid IS NULL OR id < $4::uuid) ORDER BY id DESC LIMIT $5`, province, dept, periodID, before, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Dispatch, error) { return scanDisp(r) })
}

// UpdateDispatchSend ghi trạng thái gửi.
func UpdateDispatchSend(ctx context.Context, tx pgx.Tx, d Dispatch) error {
	_, err := tx.Exec(ctx, `UPDATE pf_reports.dispatch SET status = $2, recipient_ids = $3::uuid[], cc_ids = $4::uuid[], subject = $5, body = $6,
	    link_only = $7, attempts = $8, last_error = $9, sent_at = $10, updated_at = $11 WHERE id = $1`,
		d.ID, d.Status, idStrs(d.Recipients), idStrs(d.CC), d.Subject, d.Body, d.LinkOnly, d.Attempts, d.LastError, d.SentAt, d.UpdatedAt.UTC())
	return err
}

// ClaimDispatch (P0-WORKER-REPORTS #128) nhận việc gửi một lượt bằng MỘT câu lệnh: chỉ khi lần gửi còn 'sending' đúng
// lượt attempt và chưa ai giữ hoặc hạn giữ đã qua (≤ now). Trả false khi worker khác đang giữ hoặc lượt đã xong/đổi.
// Gọi trong giao dịch riêng và commit TRƯỚC khi gửi email.
func ClaimDispatch(ctx context.Context, tx pgx.Tx, id uuid.UUID, attempt int, token uuid.UUID, until, now time.Time) (bool, error) {
	tag, err := tx.Exec(ctx, `UPDATE pf_reports.dispatch SET claim_token = $3, claim_until = $4
	  WHERE id = $1 AND status = 'sending' AND attempts = $2 AND (claim_until IS NULL OR claim_until <= $5)`,
		id, attempt, token, until.UTC(), now.UTC())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// RenewDispatchClaim gia hạn việc đang giữ; false = đã mất quyền giữ (quá hạn và worker khác đã nhận, hoặc lượt đã xong).
func RenewDispatchClaim(ctx context.Context, tx pgx.Tx, id, token uuid.UUID, until time.Time) (bool, error) {
	tag, err := tx.Exec(ctx, `UPDATE pf_reports.dispatch SET claim_until = $3 WHERE id = $1 AND claim_token = $2 AND status = 'sending'`,
		id, token, until.UTC())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// FinishDispatchClaim ghi kết quả lượt gửi ('sent' hoặc 'failed') và nhả việc — chỉ khi còn giữ đúng token.
func FinishDispatchClaim(ctx context.Context, tx pgx.Tx, d Dispatch, token uuid.UUID) (bool, error) {
	tag, err := tx.Exec(ctx, `UPDATE pf_reports.dispatch SET status = $3, last_error = $4, sent_at = $5, link_only = $6, updated_at = $7,
	    claim_token = NULL, claim_until = NULL
	  WHERE id = $1 AND claim_token = $2 AND status = 'sending'`,
		d.ID, token, d.Status, d.LastError, d.SentAt, d.LinkOnly, d.UpdatedAt.UTC())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// HasDispatchEvent cho biết sổ lần gửi đã có dòng idem_key (vd dấu recipient_mailed) — đọc mới, không qua bộ nhớ đệm.
func HasDispatchEvent(ctx context.Context, tx pgx.Tx, dispatchID uuid.UUID, idemKey string) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_reports.dispatch_event WHERE dispatch_id = $1::uuid AND idem_key = $2)`,
		dispatchID, idemKey).Scan(&ok)
	return ok, err
}

// SentForPeriod cho biết đã có lần gửi thành công cho kỳ.
func SentForPeriod(ctx context.Context, tx pgx.Tx, periodID uuid.UUID) (bool, error) {
	var ok bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pf_reports.dispatch WHERE period_id = $1::uuid AND status = 'sent')`, periodID).Scan(&ok)
	return ok, err
}

// DispatchFile là một tệp đã dựng.
type DispatchFile struct {
	ID         uuid.UUID
	DispatchID uuid.UUID
	Format     string
	FormCode   string
	FileName   string
	ObjectKey  string
	SHA256     string
	SizeBytes  int64
	CreatedAt  time.Time
}

// InsertDispatchFile ghi tệp.
func InsertDispatchFile(ctx context.Context, tx pgx.Tx, tid uuid.UUID, f DispatchFile) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_reports.dispatch_file (tenant_id, id, dispatch_id, format, form_code, file_name, object_key, sha256, size_bytes,
	    created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		tid, f.ID, f.DispatchID, f.Format, f.FormCode, f.FileName, f.ObjectKey, f.SHA256, f.SizeBytes, f.CreatedAt.UTC())
	return err
}

// DispatchFiles trả tệp của lần gửi.
func DispatchFiles(ctx context.Context, tx pgx.Tx, dispatchID uuid.UUID) ([]DispatchFile, error) {
	rows, err := tx.Query(ctx, `SELECT id, dispatch_id, format, form_code, file_name, object_key, sha256, size_bytes, created_at
	  FROM pf_reports.dispatch_file WHERE dispatch_id = $1::uuid ORDER BY form_code, format`, dispatchID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (DispatchFile, error) {
		var f DispatchFile
		err := r.Scan(&f.ID, &f.DispatchID, &f.Format, &f.FormCode, &f.FileName, &f.ObjectKey, &f.SHA256, &f.SizeBytes, &f.CreatedAt)
		return f, err
	})
}

// DispatchEvent là một dòng sổ lần gửi.
type DispatchEvent struct {
	ID         uuid.UUID
	DispatchID uuid.UUID
	Action     string
	Payload    json.RawMessage
	Department string
	Province   string
	OnBehalfOf *uuid.UUID
	EventTime  time.Time
	ActorRef   uuid.UUID
	IdemKey    string
	RecordedAt time.Time
}

// InsertDispatchEvent ghi sổ lần gửi.
func InsertDispatchEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, e *DispatchEvent) error {
	var seq int64
	p := string(e.Payload)
	if p == "" {
		p = "{}"
	}
	return tx.QueryRow(ctx, `INSERT INTO pf_reports.dispatch_event (id, tenant_id, stream_key, dispatch_id, action, payload, department_code, province_code,
	    on_behalf_of, event_time, actor_ref, idem_key, rule_version) VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, $10, $11, $12, $13)
	  RETURNING seq, recorded_at`, e.ID, tid, "dispatch:"+e.DispatchID.String(), e.DispatchID, e.Action, p, e.Department, e.Province, e.OnBehalfOf,
		e.EventTime.UTC(), e.ActorRef, e.IdemKey, DispatchRuleVersion).Scan(&seq, &e.RecordedAt)
}

// DispatchEvents trả nhật ký lần gửi (mới nhất trước).
func DispatchEvents(ctx context.Context, tx pgx.Tx, dispatchID uuid.UUID) ([]DispatchEvent, error) {
	rows, err := tx.Query(ctx, `SELECT id, dispatch_id, action, payload::text, department_code, province_code, on_behalf_of, event_time, actor_ref, idem_key,
	    recorded_at FROM pf_reports.dispatch_event WHERE dispatch_id = $1::uuid ORDER BY seq DESC`, dispatchID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (DispatchEvent, error) {
		var e DispatchEvent
		var p string
		err := r.Scan(&e.ID, &e.DispatchID, &e.Action, &p, &e.Department, &e.Province, &e.OnBehalfOf, &e.EventTime, &e.ActorRef, &e.IdemKey, &e.RecordedAt)
		e.Payload = json.RawMessage(p)
		return e, err
	})
}

// InsertGSheetLink ghi link Google Sheet đã tạo.
func InsertGSheetLink(ctx context.Context, tx pgx.Tx, tid, id, dispatchID uuid.UUID, formCode, url, shareMode string, at time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_reports.gsheet_link (tenant_id, id, dispatch_id, form_code, url, share_mode, created_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7)`, tid, id, dispatchID, formCode, url, shareMode, at.UTC())
	return err
}

// idStrs đổi mảng UUID sang chuỗi (chế độ QueryExecModeExec không biết kiểu tham số mảng).
func idStrs(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}
