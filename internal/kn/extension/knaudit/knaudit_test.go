package knaudit_test

// AUDIT-LOG-KN (#73): kiểm thử không cần DB — đăng ký nguồn hợp lệ với cổng #70, quyền xem đóng khi lỗi, biên nhận
// sai bị từ chối, câu đọc không nhắc cột văn bản tự do, và không có mã nào xóa/dọn sổ lượt xem (lưu giữ ≥ 3 tháng).
// Kiểm thử trên PostgreSQL thật ở tests/knaudit.

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knaudit"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func principal(t *testing.T, k *knport.SessionContext) authz.Principal {
	t.Helper()
	p := authz.Principal{ActorRef: ids.New(), TenantID: ids.New(), MFA: true}
	if k != nil {
		s, err := knport.NewSessionContext(*k)
		if err != nil {
			t.Fatal(err)
		}
		p.SelectedSession = &s
	}
	return p
}

func TestSourcesRegisterWithPort_AUDIT73(t *testing.T) {
	s := &knaudit.Service{}
	src := s.Sources()
	if len(src) != 6 {
		t.Fatalf("số nguồn KN = %d, muốn 6", len(src))
	}
	reg, err := accport.NewAuditRegistry(src...)
	if err != nil {
		t.Fatalf("đăng ký nguồn KN vào AuditRegistry: %v", err)
	}
	keys := map[string]string{}
	for _, x := range reg.Sources() {
		keys[x.Key] = x.Module
		if !strings.HasPrefix(x.Key, "kn.") || !strings.HasPrefix(x.Module, "kn.") {
			t.Errorf("nguồn %s/%s không mang tiền tố kn.", x.Key, x.Module)
		}
	}
	if keys[knaudit.SourceAuditView] != knaudit.ModuleAudit {
		t.Errorf("sổ lượt xem phải là một nguồn của module %s: %v", knaudit.ModuleAudit, keys)
	}
	// Bản sao: sửa map trả về không ảnh hưởng lần gọi sau.
	src[0].Fields["x"] = accport.AuditBoolean
	if _, ok := s.Sources()[0].Fields["x"]; ok {
		t.Error("Sources() phải trả bản sao fields")
	}
}

func TestAuthorizeOnlyAdminSession_AUDIT73(t *testing.T) {
	s := &knaudit.Service{} // không DB: ghi lượt bị từ chối lỗi nhưng vẫn từ chối
	if err := s.Authorize(context.Background()); err == nil {
		t.Fatal("không Principal phải bị từ chối")
	}
	if err := s.Authorize(authz.WithPrincipal(context.Background(), principal(t, nil))); err == nil {
		t.Fatal("thiếu ngữ cảnh phiên KN phải bị từ chối (không suy từ vai realm)")
	}
	p := principal(t, nil)
	p.Roles = []string{"platform_admin", "account_admin"}
	if err := s.Authorize(authz.WithPrincipal(context.Background(), p)); err == nil {
		t.Fatal("vai realm quản trị không thay được phiên Admin KN")
	}
	tinh := &knport.SessionContext{ID: ids.New(), Role: knport.SessionRoleProvince, Department: "trong_trot", OrgRef: ids.New()}
	if err := s.Authorize(authz.WithPrincipal(context.Background(), principal(t, tinh))); !isStatus(err, 403) {
		t.Fatalf("Cấp Tỉnh: muốn 403, có %v", err)
	}
	admin := &knport.SessionContext{ID: ids.New(), Role: knport.SessionRoleAdmin}
	if err := s.Authorize(authz.WithPrincipal(context.Background(), principal(t, admin))); err != nil {
		t.Fatalf("Admin KN: %v", err)
	}
	support := &knport.SessionContext{ID: ids.New(), Role: knport.SessionRoleAdmin, Support: &knport.SessionSupport{SessionID: ids.New()}}
	if err := s.Authorize(authz.WithPrincipal(context.Background(), principal(t, support))); !isStatus(err, 403) {
		t.Fatalf("Admin đang hỗ trợ: muốn 403, có %v", err)
	}
}

func TestRecordReadRejectsBadReceipt_AUDIT73(t *testing.T) {
	s := &knaudit.Service{}
	p := principal(t, &knport.SessionContext{ID: ids.New(), Role: knport.SessionRoleAdmin})
	ctx := authz.WithPrincipal(context.Background(), p)
	now := time.Date(2026, 10, 29, 3, 0, 0, 0, time.UTC)
	good := accport.AuditReadReceipt{RequestRef: ids.New(), TenantID: p.TenantID, ActorRef: p.ActorRef, ReadAt: now,
		FilterHash: strings.Repeat("a", 64), From: now.Add(-time.Hour), To: now, AsOf: now, Limit: 50,
		PurposeCode: accport.AuditPurposeView, Sources: []string{knaudit.SourceAuditView}}
	for name, mutate := range map[string]func(*accport.AuditReadReceipt){
		"người đọc khác":    func(r *accport.AuditReadReceipt) { r.ActorRef = ids.New() },
		"tenant khác":       func(r *accport.AuditReadReceipt) { r.TenantID = ids.New() },
		"mục đích khác":     func(r *accport.AuditReadReceipt) { r.PurposeCode = "export" },
		"thiếu băm":         func(r *accport.AuditReadReceipt) { r.FilterHash = "" },
		"limit 0":           func(r *accport.AuditReadReceipt) { r.Limit = 0 },
		"không nguồn":       func(r *accport.AuditReadReceipt) { r.Sources = nil },
		"thiếu request_ref": func(r *accport.AuditReadReceipt) { r.RequestRef = uuid.Nil },
		"quá 200 dòng": func(r *accport.AuditReadReceipt) {
			r.Returned = make([]accport.AuditPosition, 201)
		},
	} {
		r := good
		mutate(&r)
		if err := s.RecordRead(ctx, r); !errors.Is(err, knaudit.ErrReceipt) {
			t.Errorf("%s: muốn ErrReceipt, có %v", name, err)
		}
	}
	// Biên nhận hợp lệ nhưng chưa nối DB → lỗi (lõi #70 trả 503, không trả dữ liệu).
	if err := s.RecordRead(ctx, good); err == nil {
		t.Fatal("không DB mà ghi lượt xem thành công")
	}
}

// repoRoot tìm gốc repo (thư mục có go.mod) từ thư mục kiểm thử.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("không thấy go.mod")
		}
		dir = parent
	}
}

// Lưu giữ ≥ 3 tháng: không mã Go/SQL nào (ngoài kiểm thử) xóa, cắt, tách hay bỏ phân vùng sổ lượt xem.
func TestAuditViewNoPurgePath_AUDIT73(t *testing.T) {
	root := repoRoot(t)
	purge := regexp.MustCompile(`(?is)(delete\s+from|truncate|drop\s+table|detach\s+partition|alter\s+table)[^;]{0,200}kn_audit_view`)
	found := 0
	for _, dir := range []string{"internal", "cmd", "migrations", "scripts", "tools", "deploy"} {
		base := filepath.Join(root, dir)
		if _, err := os.Stat(base); err != nil {
			continue
		}
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || strings.HasSuffix(path, "_test.go") {
				return err
			}
			if ext := filepath.Ext(path); ext != ".go" && ext != ".sql" && ext != ".ps1" && ext != ".sh" && ext != ".yaml" && ext != ".yml" {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(b), "kn_audit_view") {
				found++
				if purge.Match(b) {
					t.Errorf("%s có lệnh xóa/dọn sổ lượt xem pf_extension.kn_audit_view", path)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if found < 2 {
		t.Fatalf("chỉ thấy %d tệp nhắc kn_audit_view — đường quét sai?", found)
	}
}

func isStatus(err error, status int) bool {
	var p *problem.Problem
	return errors.As(err, &p) && p.Status == status
}
