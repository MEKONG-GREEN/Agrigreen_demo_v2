## KN-STANDALONE-0-GOV — Quyết định KN độc lập theo BRD 2.0

Gói #261 ghi lại quyết định đã duyệt ngày 07/11/2026 trong [ADR-0058](../../adr/0058-kn-doc-lap-theo-brd-2.md), bổ sung ADR-0055. Căn cứ triển khai là Function và dữ liệu của BRD 2.0: Trồng trọt v7.7, CNTS v4.0, HCTH v4.0, Báo giá v2.0, KHTC v2.3.

### Phạm vi

- Ghi D1–D3, định nghĩa module nền/hạ tầng KN so với hệ ngoài và đủ Q2–Q10 đã duyệt.
- Ghi pha hoãn Báo giá FN-TI-02.07, phần Google Sheet của Trồng trọt FN-TI-02.06 và KNSPLIT-4B #87–#89; giữ email/link tải theo BRD.
- Ghi quyền dữ liệu, giữ lịch sử chỉ đọc và điều kiện chứng nhận bằng bảng bề mặt/profile KN; #262/#272/#273 thực hiện phần kiểm/runtime/UAT.
- Bốn tệp tài liệu: ADR, chỉ mục ADR, giả định gói và dòng roadmap. Không có dependency hoặc migration mới.

### Thứ tự dựng và kiểm

Bước 1 là tài liệu cấu trúc/chỉ mục. Các quyết định liên quan data/service/API/UI được ghi trong ADR để các gói triển khai áp dụng đúng thứ tự; gói này chưa hiện thực các lớp đó và chưa chứng nhận hoàn tất FN.

Từ worktree, dùng toolchain trong `go.mod`, chạy:

```powershell
go version
go run ./tools/docscheck
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./tools/docscheck ./tests/kn77
```

Tiếp tục bộ cổng CONTEXT, phép làm sai chỉ mục ADR có kiểm soát (cổng phải từ chối), khôi phục đúng byte và kiểm lại, CI đúng SHA, tự review rồi gộp qua helper ticket. Gói tài liệu không thêm lệnh khởi động ứng dụng; cách chạy môi trường hiện hữu ở [dev-setup](../../dev-setup.md).
