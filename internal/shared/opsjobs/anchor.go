// Package opsjobs là các job vận hành dùng chung cho mọi cụm:
//
//   - neo chuỗi hằng ngày + manifest ký số (mục 8.2, mục 22);
//   - dọn Idempotency-Key hết hạn (R-51);
//   - tạo trước phân vùng tháng cho bảng sổ (mục 8.1);
//   - xác minh toàn bộ chuỗi băm (mục 8.2, mục 22 — sau phục hồi).
//
// Hướng dẫn vận hành: docs/runbooks/.
package opsjobs

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
)

// ManifestFormat là định danh định dạng manifest neo.
const ManifestFormat = "ag.chain-anchor.v1"

// Signer ký manifest bằng Ed25519.
type Signer struct {
	key   ed25519.PrivateKey
	KeyID string
}

// NewSigner tạo Signer từ seed 32 byte hoặc khóa riêng 64 byte.
func NewSigner(raw []byte, keyID string) (*Signer, error) {
	var k ed25519.PrivateKey
	switch len(raw) {
	case ed25519.SeedSize:
		k = ed25519.NewKeyFromSeed(raw)
	case ed25519.PrivateKeySize:
		k = ed25519.PrivateKey(append([]byte(nil), raw...))
		if !bytes.Equal(ed25519.NewKeyFromSeed(k.Seed()), k) {
			return nil, errors.New("opsjobs: khóa ký Ed25519 không nhất quán")
		}
	default:
		return nil, errors.New("opsjobs: khóa ký phải là seed 32 byte hoặc khóa riêng 64 byte (base64)")
	}
	pub, _ := k.Public().(ed25519.PublicKey)
	if keyID == "" {
		keyID = KeyID(pub)
	}
	return &Signer{key: k, KeyID: keyID}, nil
}

// SignerFromEnv đọc AG_ANCHOR_SIGNING_KEY (base64 chuẩn) và AG_ANCHOR_KEY_ID (tùy chọn).
// Khóa lấy từ KMS/bí mật trong lãnh thổ; không bao giờ ghi vào log.
func SignerFromEnv() (*Signer, error) {
	v := strings.TrimSpace(os.Getenv("AG_ANCHOR_SIGNING_KEY"))
	if v == "" {
		return nil, errors.New("opsjobs: thiếu AG_ANCHOR_SIGNING_KEY")
	}
	raw, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return nil, errors.New("opsjobs: AG_ANCHOR_SIGNING_KEY không phải base64")
	}
	return NewSigner(raw, os.Getenv("AG_ANCHOR_KEY_ID"))
}

// Public trả khóa công khai — công bố cho kiểm toán qua kênh riêng.
func (s *Signer) Public() ed25519.PublicKey {
	pub, _ := s.key.Public().(ed25519.PublicKey)
	return pub
}

// KeyID là 16 ký tự hex đầu của SHA-256 khóa công khai.
func KeyID(pub ed25519.PublicKey) string {
	h := sha256.Sum256(pub)
	return hex.EncodeToString(h[:8])
}

// AnchorEntry là đầu chuỗi của một (sổ, tenant, stream) tại lúc neo.
type AnchorEntry struct {
	Ledger     string    `json:"ledger"`
	TenantID   uuid.UUID `json:"tenant_id"`
	StreamKey  string    `json:"stream_key"`
	LastSeq    int64     `json:"last_seq"`
	LastHash   string    `json:"last_hash"` // hex
	AnchoredAt time.Time `json:"anchored_at"`
}

// Manifest là nội dung được ký. Không có dữ liệu cá nhân: chỉ mã tham chiếu và băm.
type Manifest struct {
	Format       string        `json:"format"`
	Cluster      string        `json:"cluster"`
	BusinessDate string        `json:"business_date"` // YYYY-MM-DD giờ Việt Nam
	KeyID        string        `json:"key_id"`
	Count        int           `json:"count"`
	Entries      []AnchorEntry `json:"entries"`
}

// SignedManifest là tệp lưu trong kho: payload là đúng các byte đã ký.
type SignedManifest struct {
	Payload   json.RawMessage `json:"payload"`
	Alg       string          `json:"alg"`
	KeyID     string          `json:"key_id"`
	Signature string          `json:"signature"` // base64 chuẩn
}

// ManifestKey là khóa đối tượng của manifest: anchors/<cluster>/<YYYY-MM-DD>.json.
func ManifestKey(cluster string, d clock.Date) string {
	return "anchors/" + cluster + "/" + d.String() + ".json"
}

// AnchorDateFor trả ngày nghiệp vụ cần neo tại thời điểm now: ngày vừa kết
// thúc trước mốc 00:30 giờ Việt Nam gần nhất. Chạy lúc 00:30 ngày D+1 → D;
// khởi động lại lúc 00:10 ngày D+1 → D-1 (đã neo, bỏ qua).
func AnchorDateFor(now time.Time) clock.Date {
	d := clock.BusinessDate(now.Add(-30 * time.Minute))
	prev := time.Date(d.Year, d.Month, d.Day-1, 0, 0, 0, 0, time.UTC)
	return clock.Date{Year: prev.Year(), Month: prev.Month(), Day: prev.Day()}
}

// AnchorResult là kết quả neo một cụm.
type AnchorResult struct {
	Key      string
	Inserted int64 // số dòng neo mới trong shared.chain_anchor
	Entries  int   // số dòng trong manifest
	Skipped  bool  // manifest đã có từ trước: không làm gì
}

// AnchorDaily neo mọi chuỗi của một cụm cho ngày d và xuất manifest ký số.
// pool là vai ag_ops_jobs (quyền EXECUTE trên shared.anchor_chains và
// shared.chain_anchors_of — migration shared/000002). Idempotent: manifest đã
// có → bỏ qua; chạy lại sau lỗi xuất → ghi bổ sung neo thiếu rồi xuất.
func AnchorDaily(ctx context.Context, pool *pgxpool.Pool, store objstore.Store, s *Signer, cluster string, d clock.Date) (AnchorResult, error) {
	res := AnchorResult{Key: ManifestKey(cluster, d)}
	if _, err := store.Head(ctx, res.Key); err == nil {
		res.Skipped = true
		return res, nil
	} else if !errors.Is(err, objstore.ErrNotFound) {
		return res, err
	}
	if err := pool.QueryRow(ctx, "SELECT shared.anchor_chains($1::date)", d.String()).Scan(&res.Inserted); err != nil {
		return res, fmt.Errorf("opsjobs: anchor_chains: %w", err)
	}
	rows, err := pool.Query(ctx, `SELECT ledger, tenant_id, stream_key, last_seq, last_hash, anchored_at
	  FROM shared.chain_anchors_of($1::date)`, d.String())
	if err != nil {
		return res, fmt.Errorf("opsjobs: chain_anchors_of: %w", err)
	}
	m := Manifest{Format: ManifestFormat, Cluster: cluster, BusinessDate: d.String(), KeyID: s.KeyID, Entries: []AnchorEntry{}}
	for rows.Next() {
		var e AnchorEntry
		var h []byte
		if err := rows.Scan(&e.Ledger, &e.TenantID, &e.StreamKey, &e.LastSeq, &h, &e.AnchoredAt); err != nil {
			rows.Close()
			return res, err
		}
		e.LastHash = hex.EncodeToString(h)
		e.AnchoredAt = e.AnchoredAt.UTC()
		m.Entries = append(m.Entries, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return res, err
	}
	m.Count = len(m.Entries)
	res.Entries = m.Count

	body, err := SignManifest(m, s)
	if err != nil {
		return res, err
	}
	if _, err := store.Put(ctx, res.Key, bytes.NewReader(body), int64(len(body)), objstore.SHA256Hex(body), "application/json"); err != nil {
		return res, fmt.Errorf("opsjobs: xuất manifest: %w", err)
	}
	return res, nil
}

// SignManifest tuần tự hóa và ký m.
func SignManifest(m Manifest, s *Signer) ([]byte, error) {
	payload, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	sig := ed25519.Sign(s.key, payload)
	return json.MarshalIndent(SignedManifest{
		Payload: payload, Alg: "Ed25519", KeyID: s.KeyID, Signature: base64.StdEncoding.EncodeToString(sig),
	}, "", "  ")
}

// VerifyManifest kiểm chữ ký bằng khóa công khai TIN CẬY (lấy qua kênh riêng,
// không lấy từ tệp) và trả nội dung.
func VerifyManifest(b []byte, pub ed25519.PublicKey) (Manifest, error) {
	var sm SignedManifest
	if err := json.Unmarshal(b, &sm); err != nil {
		return Manifest{}, fmt.Errorf("opsjobs: manifest hỏng: %w", err)
	}
	if sm.Alg != "Ed25519" {
		return Manifest{}, errors.New("opsjobs: thuật toán ký không hỗ trợ")
	}
	sig, err := base64.StdEncoding.DecodeString(sm.Signature)
	if err != nil {
		return Manifest{}, errors.New("opsjobs: chữ ký không phải base64")
	}
	// json.Unmarshal giữ nguyên byte của RawMessage; payload do Marshal sinh đã gọn.
	var compact bytes.Buffer
	if err := json.Compact(&compact, sm.Payload); err != nil {
		return Manifest{}, err
	}
	if !ed25519.Verify(pub, compact.Bytes(), sig) {
		return Manifest{}, errors.New("opsjobs: chữ ký manifest không hợp lệ")
	}
	var m Manifest
	if err := json.Unmarshal(compact.Bytes(), &m); err != nil {
		return Manifest{}, err
	}
	if m.Format != ManifestFormat || m.Count != len(m.Entries) {
		return Manifest{}, errors.New("opsjobs: manifest sai định dạng")
	}
	return m, nil
}
