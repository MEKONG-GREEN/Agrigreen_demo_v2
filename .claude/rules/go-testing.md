---
paths:
  - "**/*_test.go"
  - "tests/**/*"
---
# Go: kiểm thử

Nguồn: [g1-conventions §5](../../docs/dev/g1-conventions.md), [package-rules §5](../../docs/process/package-rules.md), AGENTS.md §8, [transactions-and-io.md](../../docs/conventions/transactions-and-io.md), [fail-closed-config.md](../../docs/conventions/fail-closed-config.md), [g5-conventions §9](../../docs/dev/g5-conventions.md). Khi lệch, nguồn thắng.

## Hạ tầng
- Gói có test tích hợp: `func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }`.
- Pool: dựng module trên vai đăng nhập đơn vị — `env.Pool(t, migrations.PF, dbadmin.AppPlatform)` (cụm mkg: `dbadmin.AppInternal`), với `env := pgtest.Get(t)`.
- Token giả: `internal/shared/authz/authztest`; Principal HTX/xã phải đặt `OrgRef` (hoặc `KN.OrgRef`).
- Đồng hồ: `clock.NewFixed(...)`, không phụ thuộc giờ máy.

## Bắt buộc có test cho
- cách ly tenant;
- phạm vi theo cây phân cấp;
- tự phê duyệt bị chặn (nếu module có phê duyệt);
- POST lặp cùng `Idempotency-Key` trả cùng phản hồi, không ghi trùng;
- sửa sai bằng bản ghi bù (nếu có sổ);
- quy tắc nghiệp vụ chính của BRD — ghi mã FN/BR/US trong tên hoặc chú thích test;
- kiểm số ở server (âm, thiếu trường, ngoài phạm vi) cho handler/app nhận số;
- thêm theo [g5-conventions §9](../../docs/dev/g5-conventions.md) khi đụng phần G5.

## Theo loại thay đổi
- **Đổi mã trong thân transaction:** test pool nhỏ (`db.Open` với `MaxConns: 2`), gọi song song ≥ 4× kích thước pool, context có hạn — hết hạn = deadlock. Mọi module tham gia dùng **chung** pool nhỏ. Mẫu trong transactions-and-io.md.
- **Kênh ngoài/worker:** bảng test theo cấu hình (không biến → `dropped`; cấu hình một phần/sai → lỗi khởi động; không trường hợp nào `sent` khi chưa gửi); hai worker cùng claim → gửi đúng một lần.
- **RLS:** truy vấn với vai module khi bảng chiếu rỗng trả 0 hàng; có dữ liệu thì đúng phạm vi.
- **Sửa lỗi:** viết test tái hiện **đỏ trên dev trước**, xanh sau sửa; ghi cả hai kết quả vào báo cáo.

## Chạy
- Đặt `GOTOOLCHAIN` theo `go.mod` trước.
- `go test -count=1 -p 1 -timeout 55m <gói đụng tới + gói tiêu thụ trực tiếp>`; đổi menu → thêm `./internal/platform/party/...`.
- Xác nhận bằng `-v`: phải thấy `--- PASS`. **SKIP là trượt.**
- `INFRA: PostgreSQL unavailable` / lỗi 0.00s hàng loạt là hạ tầng, không phải lỗi mã: chờ `pg_isready` rồi chạy lại, hoặc dùng `pg-ephemeral`. Không dừng cụm dùng chung, không xóa database mình không tạo.
- Tên test thường mang mã gói làm hậu tố (vd `_W24`, `_WHB`) — theo cách của gói đang sửa.
