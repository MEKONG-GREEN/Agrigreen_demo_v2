package app

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/anchor/domain"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
)

type retryRegistrar struct {
	f        *fx
	refs     map[string]uuid.UUID
	calls    int
	failOnce bool
}

func (r *retryRegistrar) RegisterCooperative(_ context.Context, in partyport.CooperativeRegistration) (uuid.UUID, error) {
	r.calls++
	ref := r.refs[in.IdemKey]
	if ref == uuid.Nil {
		ref = r.f.coop(in.TaxCode)
		r.refs[in.IdemKey] = ref
	}
	if r.failOnce {
		r.failOnce = false
		return ref, problem.New(http.StatusServiceUnavailable, "party.dependency_unavailable", "lỗi thử sau khi tạo Nút")
	}
	return ref, nil
}

// ORG-NEO-001: lỗi giữa các bước không làm tạo Nút thứ hai khi gửi lại;
// quyền KN, MST sai/trùng và yêu cầu mơ hồ bị chặn trước khi gọi bên tạo Nút.
func TestCreateWithNewCooperative_ORGUI(t *testing.T) {
	f := newFx(t)
	creator := &retryRegistrar{f: f, refs: map[string]uuid.UUID{}, failOnce: true}
	f.svc.Creator = creator
	in := CreateInput{CreatedBy: domain.CreatedByMKG, MST: "0312 345-678", IdemKey: ids.New().String(),
		NewCooperative: &partyport.CooperativeRegistration{Name: "HTX tạo từ Neo", CommuneCode: "30001"}}
	if _, _, err := f.svc.Create(f.mkg(), in); code(err) != "party.dependency_unavailable" {
		t.Fatalf("gián đoạn: %v", err)
	}
	got, replayed, err := f.svc.Create(f.mkg(), in)
	if err != nil || replayed || got.MST == nil || *got.MST != "0312345678" || len(creator.refs) != 1 {
		t.Fatalf("gửi lại phải hoàn tất một Nút và một neo: %v %v", got, err)
	}
	again, replayed, err := f.svc.Create(f.mkg(), in)
	if err != nil || !replayed || got.AnchorID != again.AnchorID || creator.calls != 2 {
		t.Fatalf("phát lại neo: %v", err)
	}
	bad := in
	bad.IdemKey = ids.New().String()
	if _, _, err := f.svc.Create(f.mkg(), bad); code(err) != ErrMSTTaken.Code {
		t.Fatalf("MST trùng: %v", err)
	}
	bad.MST = "không phải MST"
	if _, _, err := f.svc.Create(f.mkg(), bad); code(err) != ErrMSTMalformed.Code {
		t.Fatalf("MST sai: %v", err)
	}
	bad = in
	bad.CreatedBy = domain.CreatedByKN
	if _, _, err := f.svc.Create(f.as(roleKNProvinceAdmin), bad); !errors.Is(err, problem.Forbidden) {
		t.Fatalf("KN tạo HTX mới: %v", err)
	}
	bad = in
	bad.OrgRef = ids.New()
	if _, _, err := f.svc.Create(f.mkg(), bad); code(err) != problem.Validation.Code {
		t.Fatalf("cả Nút cũ lẫn mới: %v", err)
	}
	if creator.calls != 2 {
		t.Fatal("yêu cầu sai không được gọi bên tạo Nút")
	}
}
