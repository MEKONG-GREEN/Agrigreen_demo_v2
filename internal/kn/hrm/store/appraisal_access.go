package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// InsertAppraisalAccess giữ phạm vi tự đánh giá đúng nghĩa. record_access cũ chỉ
// nhận all/unit cho hồ sơ; không giả scope unit để cho cán bộ xã đọc phiếu riêng.
// Không lưu tên, câu trả lời, byte tài liệu; sổ đánh giá sở hữu audit đọc/xuất này.
func InsertAppraisalAccess(ctx context.Context, tx pgx.Tx, tid uuid.UUID, a Access, at time.Time) error {
	if a.Scope != "all" && a.Scope != "unit" && a.Scope != "self" {
		return fmt.Errorf("appraisal access: invalid scope")
	}
	if a.Action != "view" && a.Action != "export" {
		return fmt.Errorf("appraisal access: invalid action")
	}
	raw, err := json.Marshal(struct {
		StaffID  *uuid.UUID        `json:"staff_id,omitempty"`
		StaffIDs []uuid.UUID       `json:"staff_ids,omitempty"`
		Count    int               `json:"item_count"`
		Scope    string            `json:"scope"`
		Query    map[string]string `json:"query"`
	}{a.StaffID, a.StaffIDs, a.ItemCount, a.Scope, a.Query})
	if err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	_, err = InsertAppraisal(ctx, tx, tid, AppraisalEvent{ID: a.ID, Kind: "access", Key: a.Actor.String(), StaffRef: a.StaffID, Action: a.Action, Payload: raw, RequestHash: hex.EncodeToString(sum[:]), IdemKey: "appraisal-access:" + a.ID.String(), Actor: a.Actor}, at)
	return err
}
