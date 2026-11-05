# Luật rút yêu cầu và tách Epic / Story / Sub-task

## 1. Danh mục yêu cầu

BRD trong dự án thường có cấu trúc: lịch sử phiên bản; tổng quan (bối cảnh, phạm vi, **vai trò**, thuật ngữ, ràng buộc/giả định); quy trình nghiệp vụ As-Is/To-Be; **danh sách yêu cầu người dùng (UR)** có cột ưu tiên; **chi tiết chức năng (FN)** gồm mô tả, người dùng, tiên quyết, luồng chính/luồng phụ, quy tắc, màn hình; yêu cầu phi chức năng. Tham khảo bản tóm tắt [docs/brd/kn-trong-trot-v7.7.md](../../../../docs/brd/kn-trong-trot-v7.7.md).

Mỗi yêu cầu một dòng:

| Cột | Cách lấy |
|---|---|
| Mã | Mã BRD (`UR-…`, `FN-…`, `BR-…`, `NFR-…`). Không có → `REQ-nnn` theo thứ tự xuất hiện |
| Mục BRD | Số mục chứa yêu cầu (vd `§4.3 FN-TI-02.04`) |
| Loại | `FR` chức năng · `BR` quy tắc nghiệp vụ · `NFR` phi chức năng · `DATA` dữ liệu/danh mục · `INT` tích hợp ngoài · `UI` khung giao diện chung |
| Ưu tiên BRD | Giữ nguyên chữ của BRD (Cao/TB/Thấp, Must/Should…); không có → trống |
| Vai trò | Người dùng của yêu cầu |
| Tiên quyết | Yêu cầu/FN phải có trước |
| Tóm tắt | Một câu, tự viết lại; **không** dán nguyên văn đoạn dài |

Quy tắc:
- UR và FN cùng nói một việc → giữ cả hai mã, nối UR ↔ FN; Story truy vết về cả hai.
- Một đoạn FN chứa nhiều quy tắc (B1…Bn, "lần đầu bắt buộc đổi mật khẩu"…) → rút từng quy tắc thành `BR` riêng có mã `<FN>-BRn` để kiểm phủ.
- Yêu cầu phi chức năng (hiệu năng, offline, thiết bị, bảo mật, nhật ký) → `NFR`; gắn vào Story liên quan hoặc thành Story kỹ thuật nếu áp dụng chung.

## 2. Epic

- **Một Epic = một năng lực nghiệp vụ** giao được độc lập, thường là một nhóm FN cùng tiền tố hoặc một phân hệ trong tên BRD. Ví dụ BRD `App HTX — Quản trị phân quyền, Hồ sơ HTX, Thống kê báo cáo` → tối thiểu 3 Epic theo 3 phân hệ.
- Phần dùng chung cho nhiều Epic (đăng nhập, khung giao diện, nhật ký thao tác, danh mục) → Epic **Nền tảng** riêng, thường ở đầu kế hoạch.
- Epic quá lớn (ước > 4 sprint hoặc > 15 Story) → tách theo vai trò hoặc theo luồng.
- Không tạo Epic cho một Story đơn lẻ; gộp vào Epic gần nhất.

## 3. Story

- **Một Story = một lát dọc mang giá trị cho một vai trò**, demo được (có giao diện hoặc API kiểm được), làm xong trong một sprint.
- Viết theo mẫu "Là `<vai trò>`, tôi muốn `<hành động>` để `<giá trị>`". Vai trò lấy đúng tên trong BRD.
- Cách tách một FN lớn (theo thứ tự ưu tiên):
  1. theo **bước luồng chính** có giá trị riêng (tạo / gửi / duyệt);
  2. theo **thao tác dữ liệu** (xem danh sách, xem chi tiết, thêm, sửa, ngừng dùng) — gộp lại nếu mỗi phần quá nhỏ (< 2 điểm);
  3. theo **vai trò** khi cùng màn nhưng quyền khác nhau;
  4. theo **quy tắc/biến thể** (luồng chính trước, luồng phụ/ngoại lệ sau);
  5. theo **kênh** (Web, Mobile) khi giao diện và ràng buộc khác nhau (offline, thiết bị).
- Mỗi Story có **3–8 tiêu chí nghiệm thu** Given/When/Then: ít nhất một luồng thành công, một trường hợp dữ liệu sai bị từ chối (kiểm ở server), một trường hợp không đủ quyền khi FN có phân quyền.
- Story chỉ có việc kỹ thuật không giá trị trực tiếp (dựng môi trường, khung giao diện, danh mục nền) được phép nhưng phải ghi lý do ở mục *Giá trị* và đặt vào Epic Nền tảng.
- Mỗi `FR`/`BR` trong danh mục phải được ít nhất một Story phủ; `NFR` phải được phủ bởi tiêu chí nghiệm thu hoặc Story kỹ thuật.

## 4. Sub-task

Sub-task là việc kỹ thuật bên trong một Story, mỗi cái do một người làm trong ≤ 2 ngày. Chọn từ các loại sau, chỉ loại thật sự cần:

| Loại | Nội dung điển hình (theo stack repo) |
|---|---|
| `DB` | Migration forward-only, bảng/sổ append-only, RLS theo tenant (AGENTS.md §7) |
| `API` | Hợp đồng OpenAPI trong `contracts/openapi/` |
| `BE` | Module Go (`port/domain/app/store/handler`), kiểm số ở server, mã lỗi `problem.*` |
| `WEB` | Màn Next.js trong `apps/web-ops`/`apps/web-internal`, dùng UI kit chung, Idempotency-Key cho lệnh ghi |
| `MOBILE` | Màn Flutter (`apps/field-app`, `packages/agrigreen_mobile`), offline nếu BRD yêu cầu |
| `TEST` | Test DB thật (`--- PASS`, không SKIP), vitest, flutter test, kịch bản UAT |
| `DOC` | Tài liệu gói, roadmap, mã lỗi, hướng dẫn |
| `OPS` | Cấu hình, biến môi trường fail-closed, tích hợp hạ tầng |

- Summary Sub-task bắt đầu bằng loại trong ngoặc vuông: `[BE] Kiểm số lượng không âm khi lưu hồ sơ`.
- Không tạo Sub-task "Review code", "Họp" — đó là quy trình, không phải việc.
- Story có `WEB` hoặc `MOBILE` ghi rõ màn/route trong mô tả Sub-task nếu BRD có tên màn.

## 5. Phụ thuộc

- Lấy từ cột *Tiên quyết* của BRD, từ quy trình To-Be (bước sau cần dữ liệu bước trước), và từ dữ liệu dùng chung (danh mục, tài khoản, phân quyền).
- Ghi phụ thuộc ở mức **Story** (`E02-S01` phụ thuộc `E01-S03`). Không để vòng phụ thuộc; gặp vòng → tách Story để phá vòng.
