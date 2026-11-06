## HCTH-V4-REMOVE-FN21-SELF — HC-0: gỡ tự đánh giá và guest-link HCTH

Gói #256, BRD HCTH v4.0 (BRD 2.0), nguồn duy nhất:
[BRD đã chốt](https://docs.google.com/document/d/1r0TqvDU8yRZSdm5BwabvWkgElm8ynC7Q/edit).
SHA256 bản DOCX: `A2201810F6BCD4DC7E6993D5F36A8618BEA7DCB9F02E98076AAC5B016E32DE14`.

### Phần Function áp dụng

- FN-16 BR-01/02/04/06: lãnh đạo nhập cho người được phân công ở MS05, HCTH nhập MS06; TW không tham gia.
- FN-17 BR-01, bảng DG-04/DG-05: lãnh đạo/HCTH nhập đề xuất cho cá nhân/tập thể. Không có actor nhân viên/Cấp Xã tự nhập.
- FN-20 BR-01 và mục quyền hệ thống: HCTH/Admin, lãnh đạo có phạm vi được giao; TW/Xã tra Contact.
- FN-21 cũ không còn trong các Function chi tiết v4.0. #200 đã đóng, HC-0 thay phần tích hợp OTP khách đã bỏ.

### Phạm vi và lựa chọn kỹ thuật

1. Không thêm dependency. Gỡ ba trang `danh-gia-cua-toi`, menu và alias; gỡ endpoint/handler/service guest-link HCTH, action `self_draft`/`self_submit` và capability `can_self`.
2. Migration mới chặn INSERT hai action đã bỏ ở DB. Sổ cũ vốn chỉ ghi thêm; không DROP/UPDATE/DELETE và không sửa migration cũ. Lịch sử điểm, payload, chuỗi băm và tham chiếu cũ vẫn đọc được bởi lãnh đạo/HCTH đúng phân công.
3. Ma trận cũ theo nhóm/người dùng không phục hồi capability của Xã, TW hoặc cán bộ Tỉnh thông thường. Không thay phạm vi các quyền hồ sơ/tài liệu khác. Thông báo bước giấy gửi lãnh đạo/HCTH, không gửi người tự đánh giá đã bị bỏ.
4. `proxy_self` của lãnh đạo có kiểm scan, các mẫu/điểm/trạng thái cũ và xuất lịch sử vẫn giữ tới HC-2. Việc bỏ toàn bộ phiếu/điểm và đổi trạng thái là phạm vi HC-2; HC-0 không tuyên bố đã hoàn thành FN-16/17/18 toàn phần.
5. Không đụng capability guestlink dùng chung của platform hoặc FN-21 chiến dịch CNTS.

### Cách chạy

Từ worktree, đặt toolchain theo `go.mod`, rồi chạy migration bằng quy trình môi trường hiện hữu. Kiểm DB riêng:

```powershell
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/hrm/... ./internal/kn/extension/menu/... ./internal/platform/party/...
```

Web:

```powershell
cd apps/web-ops
npm ci
npm run gen:api
npm run typecheck
npm test
npm run build:mock
npm run e2e -- tests/e2e/hrm-appraisal.spec.ts
npm run dev:mock
```

Không cần cấu hình hay xác minh nghiệp vụ bổ sung.
