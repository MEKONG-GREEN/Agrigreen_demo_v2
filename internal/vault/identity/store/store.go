// Package store: SQL của kho định danh, chỉ qua pool của vai vault_app (R-05).
// Mọi hàm nhận pgx.Tx đã ở ngữ cảnh tenant (tenant.WithTenantTx). Chỉ đọc/ghi
// bản mã và mã tham chiếu — không có giá trị định danh rõ ở tầng này.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Subject là dòng vault.subject.
type Subject struct {
	TenantID  uuid.UUID
	Ref       uuid.UUID
	Kind      string
	AreaCode  string
	CreatedAt time.Time
	CreatedBy uuid.UUID
	ErasedAt  *time.Time
}

// Identity là dòng vault.identity (bản mã).
type Identity struct {
	FullNameCT   []byte
	PhoneCT      []byte
	NationalIDCT []byte
	AddressCT    []byte
}

// Key là dòng vault.subject_key.
type Key struct {
	PublicKey         []byte
	WrappedPrivateKey []byte
	WrappedDataKey    []byte
	KEKID             string
}

// Blind là một dòng chỉ mục mù.
type Blind struct {
	Kind  string
	Value []byte
}

// InsertSubject ghi chủ thể mới.
func InsertSubject(ctx context.Context, tx pgx.Tx, s Subject) error {
	_, err := tx.Exec(ctx, `INSERT INTO vault.subject (tenant_id, ref, kind, area_code, created_at, created_by)
	  VALUES ($1, $2, $3, $4, $5, $6)`, s.TenantID, s.Ref, s.Kind, s.AreaCode, s.CreatedAt.UTC(), s.CreatedBy)
	return err
}

// InsertIdentity ghi bản mã định danh.
func InsertIdentity(ctx context.Context, tx pgx.Tx, tenantID, ref uuid.UUID, id Identity) error {
	_, err := tx.Exec(ctx, `INSERT INTO vault.identity (tenant_id, subject_ref, full_name_ct, phone_ct, national_id_ct, address_ct)
	  VALUES ($1, $2, $3, $4, $5, $6)`, tenantID, ref, id.FullNameCT, id.PhoneCT, id.NationalIDCT, id.AddressCT)
	return err
}

// InsertKey ghi khóa của chủ thể.
func InsertKey(ctx context.Context, tx pgx.Tx, tenantID, ref uuid.UUID, k Key) error {
	_, err := tx.Exec(ctx, `INSERT INTO vault.subject_key (tenant_id, subject_ref, public_key, wrapped_private_key, wrapped_data_key, kek_id)
	  VALUES ($1, $2, $3, $4, $5, $6)`, tenantID, ref, k.PublicKey, k.WrappedPrivateKey, k.WrappedDataKey, k.KEKID)
	return err
}

// InsertBlind ghi chỉ mục mù; false nếu (tenant, loại, giá trị) đã có.
func InsertBlind(ctx context.Context, tx pgx.Tx, tenantID, ref uuid.UUID, b Blind) (bool, error) {
	tag, err := tx.Exec(ctx, `INSERT INTO vault.blind_index (tenant_id, kind, value, subject_ref)
	  VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`, tenantID, b.Kind, b.Value, ref)
	return tag.RowsAffected() == 1, err
}

// FindBlind tìm chủ thể theo một trong các chỉ mục mù (theo thứ tự truyền vào).
func FindBlind(ctx context.Context, tx pgx.Tx, bs []Blind) (uuid.UUID, bool, error) {
	for _, b := range bs {
		var ref uuid.UUID
		err := tx.QueryRow(ctx, "SELECT subject_ref FROM vault.blind_index WHERE kind = $1 AND value = $2", b.Kind, b.Value).Scan(&ref)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return uuid.Nil, false, err
		}
		return ref, true, nil
	}
	return uuid.Nil, false, nil
}

// GetSubject đọc chủ thể; forUpdate khóa dòng (dùng khi xóa).
func GetSubject(ctx context.Context, tx pgx.Tx, ref uuid.UUID, forUpdate bool) (*Subject, error) {
	q := "SELECT tenant_id, ref, kind, area_code, created_at, created_by, erased_at FROM vault.subject WHERE ref = $1"
	if forUpdate {
		q += " FOR UPDATE"
	}
	var s Subject
	err := tx.QueryRow(ctx, q, ref).Scan(&s.TenantID, &s.Ref, &s.Kind, &s.AreaCode, &s.CreatedAt, &s.CreatedBy, &s.ErasedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// GetIdentity đọc bản mã định danh.
func GetIdentity(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*Identity, error) {
	var id Identity
	err := tx.QueryRow(ctx, `SELECT full_name_ct, phone_ct, national_id_ct, address_ct FROM vault.identity WHERE subject_ref = $1`, ref).
		Scan(&id.FullNameCT, &id.PhoneCT, &id.NationalIDCT, &id.AddressCT)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// GetKey đọc khóa của chủ thể.
func GetKey(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*Key, error) {
	var k Key
	err := tx.QueryRow(ctx, `SELECT public_key, wrapped_private_key, wrapped_data_key, kek_id FROM vault.subject_key WHERE subject_ref = $1`, ref).
		Scan(&k.PublicKey, &k.WrappedPrivateKey, &k.WrappedDataKey, &k.KEKID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &k, nil
}

// UpdateIdentity thay bản mã họ tên, SĐT, địa chỉ (giữ CCCD) — W11.22 Amend.
func UpdateIdentity(ctx context.Context, tx pgx.Tx, ref uuid.UUID, id Identity) error {
	_, err := tx.Exec(ctx, `UPDATE vault.identity SET full_name_ct = $2, phone_ct = $3, address_ct = $4, updated_at = now()
	  WHERE subject_ref = $1`, ref, id.FullNameCT, id.PhoneCT, id.AddressCT)
	return err
}

// DeleteBlind xóa chỉ mục mù loại kind của chủ thể (đổi SĐT).
func DeleteBlind(ctx context.Context, tx pgx.Tx, ref uuid.UUID, kind string) error {
	_, err := tx.Exec(ctx, "DELETE FROM vault.blind_index WHERE subject_ref = $1 AND kind = $2", ref, kind)
	return err
}

// Section là dòng vault.section (bản mã, khóa riêng của ngăn đã bọc).
type Section struct {
	PayloadCT  []byte
	WrappedKey []byte
	KEKID      string
}

// GetSection đọc ngăn của chủ thể; nil nếu chưa ghi.
func GetSection(ctx context.Context, tx pgx.Tx, ref uuid.UUID, section string) (*Section, error) {
	var x Section
	err := tx.QueryRow(ctx, `SELECT payload_ct, wrapped_key, kek_id FROM vault.section WHERE subject_ref = $1 AND section = $2`,
		ref, section).Scan(&x.PayloadCT, &x.WrappedKey, &x.KEKID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &x, nil
}

// PutSection ghi (thay) ngăn của chủ thể.
func PutSection(ctx context.Context, tx pgx.Tx, tenantID, ref uuid.UUID, section string, x Section, by uuid.UUID) error {
	_, err := tx.Exec(ctx, `INSERT INTO vault.section (tenant_id, subject_ref, section, payload_ct, wrapped_key, kek_id, updated_by)
	  VALUES ($1, $2, $3, $4, $5, $6, $7)
	  ON CONFLICT (tenant_id, subject_ref, section) DO UPDATE
	    SET payload_ct = EXCLUDED.payload_ct, wrapped_key = EXCLUDED.wrapped_key, kek_id = EXCLUDED.kek_id,
	        updated_at = now(), updated_by = EXCLUDED.updated_by`,
		tenantID, ref, section, x.PayloadCT, x.WrappedKey, x.KEKID, by)
	return err
}

// Erase xóa định danh, khóa, chỉ mục mù và đặt dấu mộ (RB-3). Chủ thể phải đã được khóa dòng.
func Erase(ctx context.Context, tx pgx.Tx, ref uuid.UUID, at time.Time, by uuid.UUID, reason string) error {
	for _, q := range []string{
		"DELETE FROM vault.blind_index WHERE subject_ref = $1",
		"DELETE FROM vault.section WHERE subject_ref = $1", // W11.22, vault 000004
		"DELETE FROM vault.identity WHERE subject_ref = $1",
		"DELETE FROM vault.subject_key WHERE subject_ref = $1",
	} {
		if _, err := tx.Exec(ctx, q, ref); err != nil {
			return err
		}
	}
	_, err := tx.Exec(ctx, `UPDATE vault.subject SET erased_at = $2, erased_by = $3, erasure_reason = $4
	  WHERE ref = $1 AND erased_at IS NULL`, ref, at.UTC(), by, reason)
	return err
}

// PubKey là một khóa công khai theo địa bàn.
type PubKey struct {
	Ref       uuid.UUID
	AreaCode  string
	PublicKey []byte
}

// ListPubKeys liệt kê khóa công khai của chủ thể chưa xóa thuộc các địa bàn, cursor theo ref (R-53).
func ListPubKeys(ctx context.Context, tx pgx.Tx, areas []string, after uuid.UUID, limit int) ([]PubKey, error) {
	rows, err := tx.Query(ctx, `SELECT s.ref, s.area_code, k.public_key
	    FROM vault.subject s JOIN vault.subject_key k ON k.tenant_id = s.tenant_id AND k.subject_ref = s.ref
	   WHERE s.erased_at IS NULL AND s.area_code = ANY($1) AND s.ref > $2
	   ORDER BY s.ref LIMIT $3`, areas, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PubKey
	for rows.Next() {
		var p PubKey
		if err := rows.Scan(&p.Ref, &p.AreaCode, &p.PublicKey); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// HouseholdPubKeys đọc khóa công khai của các chủ thể loại household chưa xóa theo ref, sắp theo ref.
func HouseholdPubKeys(ctx context.Context, tx pgx.Tx, refs []uuid.UUID) ([]PubKey, error) {
	rows, err := tx.Query(ctx, `SELECT s.ref, s.area_code, k.public_key
	    FROM vault.subject s JOIN vault.subject_key k ON k.tenant_id = s.tenant_id AND k.subject_ref = s.ref
	   WHERE s.erased_at IS NULL AND s.kind = 'household' AND s.ref = ANY($1::uuid[])
	   ORDER BY s.ref`, refStrs(refs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PubKey
	for rows.Next() {
		var p PubKey
		if err := rows.Scan(&p.Ref, &p.AreaCode, &p.PublicKey); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func refStrs(refs []uuid.UUID) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.String())
	}
	return out
}

// Access là một dòng sổ truy cập — chỉ mã tham chiếu, không có giá trị định danh.
type Access struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	StreamKey   string
	SubjectRef  *uuid.UUID
	Op          string
	Purpose     string
	Outcome     string
	TraceID     *string
	EventTime   time.Time
	ActorRef    uuid.UUID
	IdemKey     string
	RuleVersion string
	Seq         int64
	RecordedAt  time.Time
}

// AppendAccess ghi sổ truy cập; DB tính seq, prev_hash, row_hash.
func AppendAccess(ctx context.Context, tx pgx.Tx, a *Access) error {
	return tx.QueryRow(ctx, `INSERT INTO vault.access_log
	    (id, tenant_id, stream_key, subject_ref, op, purpose, outcome, trace_id, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	  RETURNING seq, recorded_at`,
		a.ID, a.TenantID, a.StreamKey, a.SubjectRef, a.Op, a.Purpose, a.Outcome, a.TraceID, a.EventTime.UTC(),
		a.ActorRef, a.IdemKey, a.RuleVersion).Scan(&a.Seq, &a.RecordedAt)
}

// AccessBySubject đọc sổ truy cập của một chủ thể theo seq.
func AccessBySubject(ctx context.Context, tx pgx.Tx, ref uuid.UUID) ([]Access, error) {
	rows, err := tx.Query(ctx, `SELECT id, tenant_id, stream_key, subject_ref, op, purpose, outcome, trace_id, event_time,
	    actor_ref, idem_key, rule_version, seq, recorded_at
	  FROM vault.access_log WHERE subject_ref = $1 ORDER BY seq`, ref)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Access
	for rows.Next() {
		var a Access
		if err := rows.Scan(&a.ID, &a.TenantID, &a.StreamKey, &a.SubjectRef, &a.Op, &a.Purpose, &a.Outcome, &a.TraceID,
			&a.EventTime, &a.ActorRef, &a.IdemKey, &a.RuleVersion, &a.Seq, &a.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
