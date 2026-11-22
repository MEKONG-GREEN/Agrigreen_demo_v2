package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// WordBinding contains only metadata attested at creation, never a live template.
// WordAt/WordActor are the exporter facts, distinct from link/audit actor/time.
type WordBinding struct {
	PeriodID        uuid.UUID `json:"period_id"`
	ExportID        uuid.UUID `json:"export_id"`
	RunID           uuid.UUID `json:"run_id"`
	ExportInputHash string    `json:"-"`
	ExportBodyHash  string    `json:"-"`
	WordVersion     int64     `json:"version"`
	Template        string    `json:"template"`
	TemplateVersion int       `json:"template_version"`
	TemplateHash    string    `json:"template_hash"`
	InputHash       string    `json:"input_hash"`
	BodyHash        string    `json:"body_hash"`
	Size            int64     `json:"size"`
	FileName        string    `json:"file_name"`
	ContentType     string    `json:"content_type"`
	DocNumber       string    `json:"doc_number"`
	DocNumberAuto   bool      `json:"doc_number_auto"`
	ActorRef        uuid.UUID `json:"actor_ref"`
	At              time.Time `json:"at"`
}

const bindingCols = `period_id,export_id,run_id,export_input_hash,export_body_hash,word_version,template_key,template_version,template_hash,word_input_hash,body_hash,body_size,file_name,content_type,doc_number,doc_number_auto,word_actor_ref,word_at`

func scanBinding(row pgx.Row) (*WordBinding, error) {
	var b WordBinding
	err := row.Scan(&b.PeriodID, &b.ExportID, &b.RunID, &b.ExportInputHash, &b.ExportBodyHash, &b.WordVersion, &b.Template, &b.TemplateVersion, &b.TemplateHash, &b.InputHash, &b.BodyHash, &b.Size, &b.FileName, &b.ContentType, &b.DocNumber, &b.DocNumberAuto, &b.ActorRef, &b.At)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &b, err
}
func WordBindingByRun(ctx context.Context, tx pgx.Tx, run uuid.UUID) (*WordBinding, error) {
	return scanBinding(tx.QueryRow(ctx, `SELECT `+bindingCols+` FROM pf_extension.khtc_word_binding WHERE run_id=$1 ORDER BY seq DESC LIMIT 1`, run))
}
func WordBindings(ctx context.Context, tx pgx.Tx, period, export uuid.UUID, offset, limit int) ([]WordBinding, error) {
	rows, err := tx.Query(ctx, `SELECT `+bindingCols+` FROM pf_extension.khtc_word_binding WHERE period_id=$1 AND export_id=$2 ORDER BY word_version DESC,run_id DESC OFFSET $3 LIMIT $4`, period, export, offset, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (WordBinding, error) {
		b, e := scanBinding(row)
		if e != nil {
			return WordBinding{}, e
		}
		return *b, nil
	})
}
func InsertWordBinding(ctx context.Context, tx pgx.Tx, tid, id, actor uuid.UUID, at time.Time, b WordBinding) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.khtc_word_binding
 (period_id,export_id,run_id,export_input_hash,export_body_hash,word_version,template_key,template_version,template_hash,word_input_hash,body_hash,body_size,file_name,content_type,doc_number,doc_number_auto,word_actor_ref,word_at,
 id,tenant_id,stream_key,event_time,actor_ref,idem_key,rule_version)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,'khtc_word_binding:'||$3::uuid::text,$21,$22,'khtc:word_binding:'||$3::uuid::text,'archive-word-v1')`,
		b.PeriodID, b.ExportID, b.RunID, b.ExportInputHash, b.ExportBodyHash, b.WordVersion, b.Template, b.TemplateVersion, b.TemplateHash, b.InputHash, b.BodyHash, b.Size, b.FileName, b.ContentType, b.DocNumber, b.DocNumberAuto, b.ActorRef, b.At.UTC(), id, tid, at.UTC(), actor)
	return err
}

type ExportFileHeader struct {
	At        time.Time
	Name      string
	ID        uuid.UUID
	PeriodID  uuid.UUID
	Version   int64
	InputHash string
	BodyHash  string
	FileName  string
	Size      int64
}

// Header avoids payload unmarshalling and byte reads during ZIP preflight.
func ArchiveFileHeader(ctx context.Context, tx pgx.Tx, period, export uuid.UUID) (*ExportFileHeader, error) {
	var h ExportFileHeader
	err := tx.QueryRow(ctx, `SELECT id,period_id,seq,input_hash,body_hash,file_name,octet_length(body),coalesce(payload->'data'->'period'->>'name',''),event_time FROM pf_extension.khtc_report_export WHERE period_id=$1 AND id=$2`, period, export).Scan(&h.ID, &h.PeriodID, &h.Version, &h.InputHash, &h.BodyHash, &h.FileName, &h.Size, &h.Name, &h.At)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &h, err
}

type ArchiveTarget struct {
	PeriodID uuid.UUID  `json:"period_id"`
	ExportID uuid.UUID  `json:"export_id"`
	RunID    *uuid.UUID `json:"run_id,omitempty"`
	Format   string     `json:"format"`
}
type ArchiveAccess struct {
	ID                        uuid.UUID
	Actor                     uuid.UUID
	Context                   uuid.UUID
	Action                    string
	PeriodID, ExportID, RunID *uuid.UUID
	Format                    string
	ExpectedBytes             int64
	FileCount                 int
	Trace                     string
	At                        time.Time
	Targets                   []ArchiveTarget
}

func InsertArchiveAccess(ctx context.Context, tx pgx.Tx, tid uuid.UUID, a ArchiveAccess) error {
	targets, err := json.Marshal(a.Targets)
	if err != nil {
		return err
	}
	if a.Targets == nil {
		targets = []byte("[]")
	}
	rule := "archive-word-v1"
	switch a.Action {
	case "read_list", "read_periods", "read_fields", "read_history", "preview_attempt", "excel_attempt":
		rule = "archive-read-v1"
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_extension.khtc_archive_access
 (context_ref,action,period_id,export_id,run_id,format,expected_bytes,file_count,trace_id,targets,id,tenant_id,stream_key,event_time,actor_ref,idem_key,rule_version)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,'khtc_archive_access:'||$13::uuid::text,$14,$13,'khtc:archive_access:'||$11::uuid::text,$15)`,
		a.Context, a.Action, a.PeriodID, a.ExportID, a.RunID, a.Format, a.ExpectedBytes, a.FileCount, a.Trace, string(targets), a.ID, tid, a.Actor, a.At.UTC(), rule)
	return err
}
