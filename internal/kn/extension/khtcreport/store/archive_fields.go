package store

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ArchiveScanQuery struct {
	Year                       int
	Kind, Search, PeriodSearch string
	Versions                   bool
	PeriodID                   uuid.UUID
}
type ArchiveCandidate struct {
	Row         ArchiveRow
	Fields      domain.ArchiveFields
	PeriodAt    time.Time
	Header      Export
	FilterValid bool
}

// Stream every matching immutable snapshot once, without a global cutoff or
// retaining payloads. Reader authority and RLS remain on the same transaction.
func walkArchive(ctx context.Context, tx pgx.Tx, q ArchiveScanQuery, visit func(ArchiveCandidate) bool) error {
	rows, err := tx.Query(ctx, `SELECT x.id,x.period_id,p.name,p.report_type,
 COALESCE(x.payload->'admin'->'document_no'->>'override',x.payload->'admin'->'document_no'->>'system',''),
 x.seq,EXISTS(SELECT 1 FROM pf_extension.khtc_report_final f WHERE f.period_id=p.id AND f.export_id=x.id),x.provisional,x.actor_ref,x.event_time,
 p.event_time,x.draft_id,x.input_hash,x.body_hash,x.file_name,x.payload,x.export_filter
 FROM pf_extension.khtc_report_export x JOIN pf_extension.khtc_period p ON p.id=x.period_id AND p.tenant_id=x.tenant_id
 WHERE p.plan_year=$1 AND ($2='' OR p.report_type=$2)
 AND ($5='00000000-0000-0000-0000-000000000000'::uuid OR p.id=$5)
 AND NOT EXISTS(SELECT 1 FROM pf_extension.khtc_period_event e WHERE e.period_id=p.id AND e.action='cancelled')
 AND ($4 OR x.id=COALESCE((SELECT f.export_id FROM pf_extension.khtc_report_final f WHERE f.period_id=p.id ORDER BY f.seq DESC LIMIT 1),
 (SELECT e.id FROM pf_extension.khtc_report_export e WHERE e.period_id=p.id ORDER BY e.seq DESC LIMIT 1)))
 AND ($3='' OR strpos(lower(p.name || ' ' || COALESCE(x.payload->'admin'->'document_no'->>'override',x.payload->'admin'->'document_no'->>'system','')),lower($3))>0)
 AND ($6='' OR strpos(lower(p.name),lower($6))>0)
 ORDER BY x.event_time DESC,x.id DESC`, q.Year, q.Kind, q.Search, q.Versions, q.PeriodID, q.PeriodSearch)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var x ArchiveCandidate
		var payload, filter []byte
		if err = rows.Scan(&x.Row.ID, &x.Row.PeriodID, &x.Row.PeriodName, &x.Row.ReportType, &x.Row.DocumentNo, &x.Row.Version, &x.Row.Final, &x.Row.Provisional, &x.Row.ActorRef, &x.Row.At, &x.PeriodAt, &x.Header.DraftID, &x.Header.InputHash, &x.Header.BodyHash, &x.Header.FileName, &payload, &filter); err != nil {
			return err
		}
		x.Fields = domain.ProjectArchiveFields(payload, filter)
		x.Row.FieldCodes = x.Fields.Codes
		x.Row.FieldStatus = x.Fields.Status
		x.Header.ID = x.Row.ID
		x.Header.PeriodID = x.Row.PeriodID
		x.Header.Version = x.Row.Version
		x.Header.Provisional = x.Row.Provisional
		x.Header.ActorRef = x.Row.ActorRef
		x.Header.At = x.Row.At
		x.FilterValid = json.Unmarshal(filter, &x.Header.Filter) == nil
		if !visit(x) {
			return nil
		}
	}
	return rows.Err()
}
func ArchiveFiltered(ctx context.Context, tx pgx.Tx, q ArchiveScanQuery, field domain.ArchiveFieldSelector, offset, limit int) ([]ArchiveRow, error) {
	out := []ArchiveRow{}
	matched := 0
	err := walkArchive(ctx, tx, q, func(x ArchiveCandidate) bool {
		if !field.Matches(x.Fields) {
			return true
		}
		if matched >= offset {
			out = append(out, x.Row)
		}
		matched++
		return len(out) < limit
	})
	return out, err
}
func ArchiveHistoryFiltered(ctx context.Context, tx pgx.Tx, q ArchiveScanQuery, field domain.ArchiveFieldSelector) ([]Export, error) {
	candidates := []ArchiveCandidate{}
	err := walkArchive(ctx, tx, q, func(x ArchiveCandidate) bool {
		if !field.Matches(x.Fields) {
			return true
		}
		candidates = append(candidates, x)
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].Header.Version > candidates[j].Header.Version })
		if len(candidates) > 100 {
			candidates = candidates[:100]
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	out := []Export{}
	for _, x := range candidates {
		if !x.FilterValid {
			return nil, errors.New("invalid frozen export filter")
		}
		out = append(out, x.Header)
	}
	return out, nil
}
func ArchivePeriodChoices(ctx context.Context, tx pgx.Tx, q ArchiveScanQuery, field domain.ArchiveFieldSelector, offset, limit int) ([]ArchivePeriod, error) {
	type candidate struct {
		period ArchivePeriod
		at     time.Time
	}
	byID := map[uuid.UUID]candidate{}
	err := walkArchive(ctx, tx, q, func(x ArchiveCandidate) bool {
		if field.Matches(x.Fields) {
			byID[x.Row.PeriodID] = candidate{ArchivePeriod{ID: x.Row.PeriodID, Name: x.Row.PeriodName, ReportType: x.Row.ReportType}, x.PeriodAt}
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	all := make([]candidate, 0, len(byID))
	for _, x := range byID {
		all = append(all, x)
	}
	sort.Slice(all, func(i, j int) bool {
		if !all[i].at.Equal(all[j].at) {
			return all[i].at.After(all[j].at)
		}
		return all[i].period.ID.String() > all[j].period.ID.String()
	})
	out := []ArchivePeriod{}
	for i := offset; i < len(all) && len(out) < limit; i++ {
		out = append(out, all[i].period)
	}
	return out, nil
}

type ArchiveFieldChoice struct {
	Code string `json:"code"`
}

func ArchiveFieldChoices(ctx context.Context, tx pgx.Tx, q ArchiveScanQuery, field domain.ArchiveFieldSelector, after string, limit int) ([]ArchiveFieldChoice, error) {
	// Keep only the next bounded lexical page while examining every matching
	// export. This cannot starve an older code beyond10,000 newer snapshots.
	keep := map[string]bool{}
	err := walkArchive(ctx, tx, q, func(x ArchiveCandidate) bool {
		if !field.Matches(x.Fields) {
			return true
		}
		for _, code := range x.Fields.Codes {
			if code <= after || keep[code] {
				continue
			}
			keep[code] = true
			if len(keep) > limit {
				keys := make([]string, 0, len(keep))
				for k := range keep {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				delete(keep, keys[len(keys)-1])
			}
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(keep))
	for k := range keep {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]ArchiveFieldChoice, 0, len(keys))
	for _, code := range keys {
		out = append(out, ArchiveFieldChoice{Code: code})
	}
	return out, nil
}
func ArchiveFieldsByID(ctx context.Context, tx pgx.Tx, period, export uuid.UUID) (*domain.ArchiveFields, error) {
	var payload, filter []byte
	err := tx.QueryRow(ctx, `SELECT payload,export_filter FROM pf_extension.khtc_report_export WHERE period_id=$1 AND id=$2`, period, export).Scan(&payload, &filter)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	meta := domain.ProjectArchiveFields(payload, filter)
	return &meta, nil
}
