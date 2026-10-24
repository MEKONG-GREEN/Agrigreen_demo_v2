package app_test

// P0-POOL-SHARED (#126): dựng tập dữ liệu KHÔNG ghi kho đối tượng khi đang giữ giao dịch DB — pool CỠ 1 vẫn phục vụ
// giao dịch khác trong lúc ghi ảnh chụp treo; phiên bản chỉ trỏ tới ảnh chụp khớp băm; ghi lỗi → không phiên bản.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// hookStore bọc kho đối tượng: before chạy trước mỗi Put (chặn, tiêm lỗi, mô phỏng lần dựng song song).
type hookStore struct {
	objstore.Store
	puts   atomic.Int32
	before func(n int32) error
}

func (h *hookStore) Put(ctx context.Context, key string, r io.Reader, size int64, sum, ct string) (objstore.Info, error) {
	n := h.puts.Add(1)
	if h.before != nil {
		if err := h.before(n); err != nil {
			return objstore.Info{}, err
		}
	}
	return h.Store.Put(ctx, key, r, size, sum, ct)
}

func datasetVersions(t *testing.T, f *feedFixture, b tenant.Beginner) []store.DatasetVersion {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out []store.DatasetVersion
	if err := tenant.WithTenantTx(ctx, b, f.op, func(tx pgx.Tx) error {
		for v := int64(1); ; v++ {
			dv, err := store.DatasetVersionByNo(ctx, tx, port.FeedHarvestProgress, v)
			if err != nil || dv == nil {
				return err
			}
			out = append(out, *dv)
		}
	}); err != nil {
		t.Fatalf("đọc dataset_version: %v", err)
	}
	return out
}

func snapshotSHA(t *testing.T, f *feedFixture, key string) string {
	t.Helper()
	rc, _, err := f.store.Get(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	h := sha256.New()
	if _, err := io.Copy(h, rc); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestFeedSnapshotPutOutsideTxOnSingleConnPool(t *testing.T) {
	f := newFeedFixture(t)
	sized := pgtest.Get(t).SizedPool(t, migrations.PF, port.DBRole, 1)
	f.svc.DB = sized
	entered, release := make(chan struct{}), make(chan struct{})
	f.svc.Store = &hookStore{Store: f.store, before: func(int32) error {
		close(entered)
		<-release
		return nil
	}}
	f.src.set(f.t1, "89001", 12)
	type out struct {
		sha, key string
		err      error
	}
	done := make(chan out, 1)
	go func() {
		r, err := f.svc.BuildAggregate(context.Background(), port.FeedHarvestProgress)
		done <- out{r.SHA256, r.SnapshotKey, err}
	}()
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("không ghi ảnh chụp")
	}
	// Ghi kho đối tượng đang treo: kết nối DUY NHẤT của pool phải rảnh, chưa có phiên bản nào.
	if vs := datasetVersions(t, f, sized); len(vs) != 0 {
		t.Fatalf("phiên bản ghi trước khi có ảnh chụp: %d", len(vs))
	}
	close(release)
	r := <-done
	if r.err != nil {
		t.Fatal(r.err)
	}
	vs := datasetVersions(t, f, sized)
	if len(vs) != 1 || vs[0].SnapshotKey == nil || *vs[0].SnapshotKey != r.key || vs[0].SHA256 != r.sha {
		t.Fatalf("phiên bản/ảnh chụp: %+v", vs)
	}
	if got := snapshotSHA(t, f, r.key); got != r.sha || vs[0].SnapshotSize == nil {
		t.Fatal("ảnh chụp không khớp tổng kiểm")
	}
	// Không đổi → không phiên bản mới, không ghi kho thêm.
	if _, err := f.svc.BuildAggregate(context.Background(), port.FeedHarvestProgress); err != nil {
		t.Fatal(err)
	}
	if n := f.svc.Store.(*hookStore).puts.Load(); n != 1 {
		t.Fatalf("ghi kho %d lần, muốn 1", n)
	}
}

// Lần dựng khác thắng trong lúc ghi ảnh chụp: lần dựng này dựng lại, thấy không đổi → không phiên bản thứ hai; phiên
// bản duy nhất trỏ tới ảnh chụp khớp băm của nó.
func TestFeedSnapshotConcurrentBuildWins(t *testing.T) {
	f := newFeedFixture(t)
	f.src.set(f.t1, "89001", 12)
	other := *f.svc
	other.Store = f.store
	f.svc.Store = &hookStore{Store: f.store, before: func(n int32) error {
		if n == 1 {
			if _, err := other.BuildAggregate(context.Background(), port.FeedHarvestProgress); err != nil {
				return err
			}
		}
		return nil
	}}
	if _, err := f.svc.BuildAggregate(context.Background(), port.FeedHarvestProgress); err != nil {
		t.Fatal(err)
	}
	vs := datasetVersions(t, f, f.svc.DB)
	if len(vs) != 1 || vs[0].SnapshotKey == nil {
		t.Fatalf("muốn đúng 1 phiên bản có ảnh chụp: %+v", vs)
	}
	if snapshotSHA(t, f, *vs[0].SnapshotKey) != vs[0].SHA256 {
		t.Fatal("phiên bản trỏ ảnh chụp lệch băm")
	}
}

// Ghi ảnh chụp lỗi → lỗi lần dựng, KHÔNG phiên bản, KHÔNG mục feed (giao dịch đã hủy trước khi ghi kho).
func TestFeedSnapshotPutFailureWritesNothing(t *testing.T) {
	f := newFeedFixture(t)
	f.src.set(f.t1, "89001", 12)
	f.svc.Store = &hookStore{Store: f.store, before: func(int32) error { return errors.New("fixture: kho đối tượng lỗi") }}
	if _, err := f.svc.BuildAggregate(context.Background(), port.FeedHarvestProgress); err == nil {
		t.Fatal("lỗi kho đối tượng bị nuốt")
	}
	if vs := datasetVersions(t, f, f.svc.DB); len(vs) != 0 {
		t.Fatalf("phiên bản ghi dù ảnh chụp lỗi: %d", len(vs))
	}
	if p := f.read(t, nil, 10); len(p.Entries) != 0 {
		t.Fatalf("mục feed ghi dù ảnh chụp lỗi: %d", len(p.Entries))
	}
	// Kho hồi phục → dựng lại bình thường.
	f.svc.Store = f.store
	r, err := f.svc.BuildAggregate(context.Background(), port.FeedHarvestProgress)
	if err != nil || r.Version != 1 || r.SnapshotKey == "" {
		t.Fatalf("dựng lại sau lỗi: %+v %v", r, err)
	}
}
