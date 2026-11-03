package app

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	"github.com/jackc/pgx/v5"
)

type archivePreparedFile struct {
	Selection ArchiveSelection
	Header    store.ExportFileHeader
	Word      *store.WordBinding
	Size      int64
	Name      string
	At        time.Time
}

func (s *Service) prepareArchiveZIP(ctx context.Context, c caller, in []ArchiveSelection) ([]archivePreparedFile, error) {
	files := make([]archivePreparedFile, 0, len(in))
	var total int64
	err := s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		targets := make([]store.ArchiveTarget, 0, len(in))
		for _, selection := range in {
			if selection.Format == "" {
				selection.Format = "excel"
			}
			header, err := archiveHeader(ctx, tx, selection.PeriodID, selection.ExportID)
			if err != nil {
				return err
			}
			file := archivePreparedFile{Selection: selection, Header: *header, Size: header.Size, Name: fmt.Sprintf("bao-cao-%s-v%d.xlsx", header.ID, header.Version), At: header.At}
			target := store.ArchiveTarget{PeriodID: selection.PeriodID, ExportID: selection.ExportID, Format: selection.Format}
			if selection.Format == "word" {
				if s.Exporter == nil {
					return ErrDependency
				}
				file.Word, err = boundWordMetadata(ctx, tx, header, selection.RunID)
				if err != nil {
					return err
				}
				file.Size = file.Word.Size
				file.At = file.Word.At
				file.Name = fmt.Sprintf("bao-cao-%s-v%d-word-%s.docx", header.ID, header.Version, selection.RunID)
				id := selection.RunID
				target.RunID = &id
			}
			if file.Size <= 0 || file.Size > int64(ArchiveMaxBytes)-total {
				return ErrInvalid.WithDetail("Tổng dung lượng vượt 20 MiB; chọn ít tệp hơn")
			}
			total += file.Size
			files = append(files, file)
			targets = append(targets, target)
		}
		// Validate the whole batch and budget first. Commit attempts atomically before
		// any stored-byte I/O; no event claims successful network transmission.
		for i, file := range files {
			if err := s.auditArchive(ctx, tx, c, "zip_entry_attempt", file.Selection, file.Size, 1, targets[i:i+1]); err != nil {
				return err
			}
		}
		return s.auditArchive(ctx, tx, c, "zip_attempt", ArchiveSelection{Format: "zip"}, total, len(files), targets)
	})
	return files, err
}

func (s *Service) readArchiveExcel(ctx context.Context, c caller, file archivePreparedFile) ([]byte, error) {
	var body *store.ArchiveStoredBody
	err := s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		current, e := archiveHeader(ctx, tx, file.Header.PeriodID, file.Header.ID)
		if e != nil {
			return e
		}
		if current.InputHash != file.Header.InputHash || current.BodyHash != file.Header.BodyHash || current.Size != file.Size {
			return ErrDependency
		}
		body, e = store.ArchiveBody(ctx, tx, file.Header.PeriodID, file.Header.ID)
		return e
	})
	if err != nil {
		return nil, err
	}
	if body == nil {
		return nil, ErrNotFound
	}
	if int64(len(body.Body)) != file.Size || body.InputHash != file.Header.InputHash || body.BodyHash != file.Header.BodyHash || body.Version != file.Header.Version || body.FileName != file.Header.FileName {
		return nil, ErrDependency
	}
	sum := sha256.Sum256(body.Body)
	if hex.EncodeToString(sum[:]) != file.Header.BodyHash {
		return nil, ErrDependency.WithDetail("Excel đã lưu không khớp băm")
	}
	return body.Body, nil
}

type archiveBudgetBuffer struct {
	body  bytes.Buffer
	limit int
}

func (b *archiveBudgetBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.body.Len() {
		return 0, ErrInvalid.WithDetail("Dung lượng ZIP vượt 20 MiB; chọn ít tệp hơn")
	}
	return b.body.Write(p)
}

func (b *archiveBudgetBuffer) Bytes() []byte { return b.body.Bytes() }

// ArchiveZIP copies already stored Word/Excel bytes. Metadata and total size are
// authorized before reads, each body is checked before copy and the ZIP writer
// itself has a hard byte budget (including container overhead).
func (s *Service) ArchiveZIP(ctx context.Context, in []ArchiveSelection) ([]byte, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	if err = ValidateArchiveSelection(in); err != nil {
		return nil, err
	}
	files, err := s.prepareArchiveZIP(ctx, c, in)
	if err != nil {
		return nil, err
	}
	buffer := &archiveBudgetBuffer{limit: ArchiveMaxBytes}
	writer := zip.NewWriter(buffer)
	var copied int64
	for _, file := range files {
		var body []byte
		if file.Word != nil {
			run, e := s.readBoundWord(ctx, *file.Word)
			if e != nil {
				return nil, e
			}
			body = run.Body
		} else {
			body, err = s.readArchiveExcel(ctx, c, file)
			if err != nil {
				return nil, err
			}
		}
		if int64(len(body)) != file.Size || int64(len(body)) > int64(ArchiveMaxBytes)-copied {
			return nil, ErrDependency.WithDetail("Kích thước tệp lưu không khớp")
		}
		entry, e := writer.CreateHeader(&zip.FileHeader{Name: file.Name, Method: zip.Store, Modified: file.At})
		if e != nil {
			return nil, e
		}
		count, e := io.Copy(entry, io.LimitReader(bytes.NewReader(body), int64(ArchiveMaxBytes)-copied+1))
		if e != nil {
			return nil, e
		}
		if count != file.Size {
			return nil, ErrDependency
		}
		copied += count
	}
	if err = writer.Close(); err != nil {
		return nil, err
	}
	if err = s.archiveEgress(ctx, c, int64(len(files)), int64(len(buffer.Bytes()))); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
