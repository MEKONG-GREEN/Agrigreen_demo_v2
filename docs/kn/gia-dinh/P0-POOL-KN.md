## P0-POOL-KN — Gỡ transaction lồng nhau khi mở phiếu và mở lại kỳ (#125)

Lỗi (báo cáo Ecombox §3, đối chiếu lại trên dev 719b88b3): vòng đời báo cáo KN (`internal/kn/extension/reportcycle/app`) giữ một
giao dịch tenant (`s.tx`) rồi gọi port của module khác — `Forms.FormVersion` (pf/reports), `Dispatches.SentForPeriod`
(pf/reports), `Regions.*`/`Units.Get` (pf/party) — mà mỗi port mở giao dịch RIÊNG trên CHUNG pool của gốc lắp ráp
(ADR-0028). N thao tác song song với N = cỡ pool giữ hết kết nối và cùng chờ kết nối thứ hai → treo toàn API tới hết hạn.

Sửa (chỉ làn kn, không đụng `internal/platform/**`): `app/deps.go` — `extDeps` gom dữ liệu module khác, nạp NGOÀI giao
dịch; `txDeps` chạy thân giao dịch với `extDeps` "niêm phong": thiếu phiên bản mẫu / sổ khu vực tỉnh → hủy giao dịch
(`errNeedDeps`), nạp ngoài giao dịch, chạy lại thân giao dịch từ đầu (đọc lại và kiểm lại mọi thứ — vd phiên bản mẫu của
công việc); thiếu tên xã → ghi nhận, giao dịch hủy ở cuối rồi chạy lại. `want`/`wantPeriod` khai báo sớm để hủy trước khi ghi.

| Câu hỏi / mục | Lựa chọn đã dùng | Ở đâu | Đổi thế nào |
|---|---|---|---|
| Mở phiếu (GetTask) biểu nhiều kỳ | Lần 1 đọc công việc, thiếu phiên bản mẫu → hủy, nạp ngoài giao dịch; lần 2 đọc lại công việc + giá trị khe kỳ khác trong MỘT giao dịch (nhất quán như cũ) | `task.go` `GetTask` | — |
| Mở lại kỳ: "kỳ đã gửi Chi cục" | Đọc `SentForPeriod` TRƯỚC giao dịch mở lại (sự kiện mở lại kỳ trong outbox vẫn mang `dispatch_changed`, cùng giao dịch). Gửi Chi cục chen giữa hai bước → cờ cảnh báo trễ một lần, như gửi ngay sau khi mở lại. Kiểm phạm vi kỳ vẫn trong giao dịch; idempotency HTTP không đổi | `review.go` `ReopenPeriod` | Nếu PO cần cờ chính xác tuyệt đối: pf/reports cung cấp đọc qua `db.JoinTx` (làn shared) |
| Gửi/Lưu nháp (Submit), Chốt kỳ, Chốt lại, "Cập nhật" tổng hợp, job tự chốt/tự chốt lại, Thống kê xã, đồng bộ công việc | Cùng cơ chế `txDeps`: phiên bản mẫu, khu vực, tên xã nạp ngoài giao dịch; thân giao dịch chạy lại tối đa 5 lần, quá → 503 `extension.dependency_unavailable` | `task.go`, `review.go`, `consolidate.go`, `job.go`, `dashboard.go`, `sync.go` | Tăng `maxDepsAttempts` |
| Tính đúng khi dữ liệu module khác đổi giữa hai lần | Phiên bản mẫu là bất biến theo id; khu vực/tên xã đọc lúc thao tác như trước (trước đây cũng đọc ở giao dịch riêng của pf/party, không cùng ảnh chụp) | — | — |
| Test tái hiện | `tests/kn77/pool_nested_tx_integration_test.go`: pf/extension + pf/reports chung pool app_platform 2 kết nối; 24 lời gọi song song (mở phiếu, mở lại kỳ, lưu nháp, Cập nhật). Trên dev 719b88b3: 24/24 treo tới hết hạn 10 s; sau sửa: đạt | fixture `sharedPool` | — |
| Chỗ khác trong `internal/kn` còn gọi port trong giao dịch (CHƯA sửa ở gói này — rà tĩnh theo tên, cần ticket tiếp) | `extension/app`: `Scope.Scope`, `Units.Get`, `Params.At` trong `directives.go`, `escalation.go`, `reports.go`, `roster.go`, `schedule.go` (+ `Orgs.Get`, `ContactNames`), `sync.go`, `tasks.go`, `gpslog.go` (`GpsLog.RecordFix`); `cntsalert/app` `config.go`, `evaluate.go` (`Feed.*`, `Params.At`, `Units.Get`); `cntscal/app/vaccination.go` (`Catalogs.ListAt`); `cntsmodel/app` `grants.go`, `households.go`, `projects.go`, `sites.go` (`Catalogs.*`, `Officers.XaScopes`, `Units.Get`, `Vault.Display`, `Programs.Program`); `khtcperiod/app/progress.go` (`Control.*`, `Reports.HasFinal`, `Crosswalk.States`, `Departments`, `Params.At`); `hrm/app` (`Access.Session`) | — | Ticket tiếp theo: áp cùng mẫu (nạp ngoài giao dịch) hoặc port hỗ trợ `db.JoinTx` |
