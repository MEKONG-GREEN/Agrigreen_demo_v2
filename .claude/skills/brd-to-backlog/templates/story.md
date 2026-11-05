# Template Story

Trường Jira: **Issue type** `Story` · **Parent** Epic · **Summary** hành động của người dùng, bắt đầu bằng động từ (≤ 80 ký tự, không mã tạm) · **Priority** · **Story point** · **Sprint** · **Labels** `brd-<mã-hệ>`.

Mô tả (Description):

```markdown
## User story
Là **<vai trò>**, tôi muốn **<hành động>** để **<giá trị>**.

## Tiêu chí nghiệm thu
**AC1 — <tên ngắn, luồng thành công>**
- Given <ngữ cảnh/dữ liệu sẵn có>
- When <hành động>
- Then <kết quả quan sát được>

**AC2 — <dữ liệu không hợp lệ bị từ chối ở server>**
- Given …
- When …
- Then …

**AC3 — <không đủ quyền>** (khi chức năng có phân quyền)
- Given …
- When …
- Then …

## Quy tắc nghiệp vụ
- <BR: điều kiện, giới hạn, công thức — mỗi quy tắc một dòng, mã `<FN>-BRn` nếu có>

## Dữ liệu và kiểm nhập
| Trường | Kiểu | Bắt buộc | Kiểm (server) |
|---|---|---|---|
| <tên> | <chuỗi/số/ngày/danh mục> | có/không | <không âm, độ dài, định dạng, thuộc danh mục> |

## Phi chức năng
- <hiệu năng, offline, thiết bị, nhật ký thao tác, bảo mật — chỉ khi áp dụng>

## Lưu ý kỹ thuật bắt buộc (AGENTS.md §9)
- <chỉ ghi mục áp dụng: kiểm số ở server; một Idempotency-Key mỗi thao tác ghi; không I/O ngoài trong transaction; cấu hình gửi email/thông báo fail-closed; RLS theo tenant>

## Truy vết BRD
- Mục: <§…> · Yêu cầu: <UR-…, FN-…, BR-…>
- Màn hình/wireframe: <tên màn trong BRD, nếu có>

## Phụ thuộc
- <Story phải xong trước — mã Jira hoặc mã tạm>

## Kế hoạch
- Lý do priority: <một dòng>
- Story point: <n> (<"ước tạm" nếu chưa chắc>)

## Giả định / câu hỏi mở
- Giả định: <…>
- <Qn: …>
```

Ví dụ summary: `Tạo tài khoản và gán vai trò cho thành viên HTX`, `Xem và cập nhật hồ sơ HTX`, `Xuất báo cáo thống kê thành viên theo tháng`.

Ví dụ một tiêu chí:

```markdown
**AC2 — Từ chối vốn điều lệ âm**
- Given quản trị HTX đang sửa hồ sơ HTX
- When gửi vốn điều lệ = -1.000.000 qua API (bỏ qua kiểm giao diện)
- Then API trả lỗi kiểm nhập, hồ sơ không đổi, giao diện hiện lỗi tại trường "Vốn điều lệ"
```
