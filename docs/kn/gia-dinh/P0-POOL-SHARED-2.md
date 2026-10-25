## P0-POOL-SHARED-2 — I/O ngoài trong consumer inbox/outbox và hai chỗ KN bàn giao từ #139 (#143)

Tiếp #126 (P0-POOL-SHARED) và #139 (P0-POOL-KN-2). Nguyên tắc như #126: không gọi kho đối tượng, SMTP, danh bạ, hay port module
khác (tự mở giao dịch trên CÙNG pool — ADR-0028) khi đang giữ giao dịch DB. Khác #126: ở đây là handler inbox, nơi kết quả, dòng
inbox và outbox phải commit cùng nhau (R-61), nên sửa ngay ở mẫu consumer (`inbox.Process`) thay vì từng handler tự tách giao dịch.

Cơ chế: `internal/kn/extension/txdeps` (#139) chuyển thành `internal/shared/txdeps` (KN đổi đường import, không đổi hành vi).
`inbox.Process` gắn bộ nhớ txdeps MỚI cho mỗi thông điệp và chạy thân giao dịch (ghi dòng inbox + handler) bằng
`txdeps.RunMax`. Handler gọi ra ngoài qua `txdeps.Get(ctx, khóa, nạp)`: lần đầu trả `ErrNeed` → giao dịch inbox HỦY (chưa có
dòng inbox), `nạp` chạy NGOÀI giao dịch đúng một lần, rồi handler chạy lại trong giao dịch MỚI, lấy kết quả đã nhớ và chỉ ghi
kết quả + inbox + outbox. Handler không dùng `txdeps.Get` chạy một lần như trước.

| Câu hỏi / mục | Lựa chọn đã dùng | Ở đâu | Đổi thế nào |
|---|---|---|---|
| Sửa ở mẫu consumer hay từng handler | Ở mẫu: `inbox.Process` + `txdeps`. Không đổi chữ ký `inbox.Handler`, không đổi đấu nối worker/assembly. Handler gọi trực tiếp với `tx` (không bộ nhớ, như test cũ) giữ hành vi cũ (`Get` nạp ngay) | `shared/inbox/inbox.go`, `shared/txdeps` | — |
| Số lần chạy thân giao dịch | `inbox.MaxTxAttempts = 10` (mặc định `txdeps.MaxAttempts = 5` cho thao tác API). Cảnh báo CNTS có tới 4–5 tầng đọc phụ thuộc nhau (bài nộp → tham số → bài kỳ trước → danh bạ → bài BM-01 trong tuần). Quá → `txdeps.ErrExhausted`, thông điệp NACK, giao lại | `inbox.go` | Đổi hằng |
| Run lồng | Trước: `txdeps.Run` trong thân đang niêm phong trả lỗi "Run lồng". Nay chạy MỘT lần, dùng chung bộ nhớ; khóa thiếu lan lên Run ngoài cùng. Cần vì handler inbox có thể gọi use case tự chạy `Run` | `txdeps.RunMax` | — |
| Panic trong `nạp` | Thành lỗi của khóa (`txdeps: load panic`); handler chạy lại nhận lỗi đó → NACK/lùi/DLQ như `recover` của worker pf/reports (#128). Cần vì `nạp` chạy ngoài thân handler, ngoài `recover` của worker | `txdeps.safeLoad` | — |
| Xuất Public API (`apiclient/app/export.go`) | Đọc ô nguồn (`Collection.CollectionCells`, port module chủ dữ liệu) và `Store.Put` qua `txdeps.Get`. Khóa `Put` gồm khóa đối tượng + SHA-256: lần chạy lại dựng lại tệp từ phần tử đã nhớ, cùng băm → cùng kết quả. `ErrExists` (khác nội dung) → `apiclient.export_conflict` như trước | `HandleExportRequested` | — |
| Xuất gói MRV (`mrv/app/export.go`) | `Store.Put` qua `txdeps.Get` (khóa + băm gói). Ảnh chụp dựng từ sổ trong giao dịch, tất định tại `as_of` — lần chạy lại cùng băm | `HandleExportRequested` | — |
| Dựng tệp báo cáo (`reports/app/worker.go`) | `buildDataset` (cổng module nguồn: KN, mechanization, cropcycle, tên đơn vị) và `Store.Put` qua `txdeps.Get` | `HandleExportRequested` | — |
| Email lịch báo cáo (`reports/app/delivery.go`) | Ký URL, `Directory.EmailOf`, `Mailer.Send` trong MỘT `nạp` (`deliver`): giao dịch riêng NHẬN việc gửi (`schedule_run.delivery_claim_id/_until`, migration pf `20261025135114_pf_reports_delivery_claim`, hạn 1 phút) → gửi (hạn lời gọi = hạn nhận − biên `dispatch.SendMargin`) → handler chạy lại ghi `MarkDelivered` (bỏ nhận) cùng inbox. Gửi lỗi → trả nhận (giao dịch riêng, ctx không hủy), lỗi → giao lại gửi lại | `HandleExportCompleted`, `deliver`, `store.ClaimDelivery` | — |
| Vì sao cần nhận việc cho email lịch | Trước: hai lần giao đồng thời bị dòng inbox / `FOR UPDATE` của `schedule_run` xếp hàng nên không gửi trùng. Bỏ giao dịch giữ suốt thì phải có khóa bền. Worker khác đang giữ còn hạn → CHỜ (0,5 s/lần) tới khi có kết quả gửi (bỏ qua) hoặc hết hạn (nhận lại) — không NACK ngay để giao lại sau khi worker giữ chết không rơi vào DLQ trong cửa sổ lùi ~15 s | `deliver` | Đổi `deliveryLease` |
| Email lịch: ít nhất một lần | Gửi xong mà ghi kết quả lỗi (DB) → giao lại chờ hết hạn nhận rồi gửi lại. Gửi một phần người nhận rồi lỗi → giao lại gửi lại cả lượt (như trước). Dấu từng người nhận như gửi Chi cục (#128) là gói sau nếu cần | — | Thêm sổ dấu người nhận |
| Gửi Chi cục (`reports/app/dispatch.go`) | Cả lượt gửi (#128: đọc, nhận việc, đọc kho, SMTP, dấu người nhận, kết quả — mỗi bước một giao dịch ngắn riêng) chạy trong MỘT `nạp` (`sendDispatch`), khóa theo event_id. Giao dịch inbox không còn mở suốt lượt gửi (trước: giữ một kết nối trong khi chờ kết nối thứ hai cho giao dịch riêng, và trong lúc SMTP). Danh bạ và tệp đã xuất đọc cùng giao dịch nhận việc thay vì giao dịch inbox. Ngữ nghĩa nhận việc/gia hạn/dấu/kết quả của #128 giữ nguyên | `HandleDispatchRequested`, `sendDispatch` | — |
| Liên kết tải Chi cục (`reports/app/dispatch_link.go`) | Không phải consumer (API công khai). Giao dịch 1 chỉ đọc (liên kết, tệp); mở tệp ở kho NGOÀI giao dịch; giao dịch 2 đọc lại liên kết (có thể vừa thu hồi/hết hạn), qua điểm kiểm soát xuất, ghi "served" + export_audit cùng giao dịch như trước. Kho lỗi → không ghi sổ (như trước). Điểm kiểm soát từ chối → đóng tệp đã mở, ghi `egress_denied` ở giao dịch riêng như trước | `OpenDispatchLink` | — |
| Cảnh báo CNTS (`cntsalert/app/evaluate.go`, bàn giao #139) | Mọi port module khác qua `txdeps.Get`: `Feed.Recorded`, `Feed.Latest` (khóa = JSON của truy vấn), `Params.At`; `Units.Get`, `Dir.Officers`, `Dir.FormOwners` vốn đã qua `txdeps.Get` từ #139 nhưng trước đây ctx inbox không có bộ nhớ nên vẫn gọi trong giao dịch | `HandleEvent`, `latest`, `threshold` | — |
| Log GPS yêu cầu hỗ trợ (`extension/app/gpslog.go`, bàn giao #139) | `GpsLog.RecordFix` (pf/geo, giao dịch riêng) qua `txdeps.Get` khóa = IdemKey; geo chống trùng theo IdemKey nên giao lặp an toàn | `HandleGpsLog` | — |
| Hủy kỳ KHTC (bàn giao #139) | Không dùng `db.JoinTx` (cap/forms hủy kỳ native ghi bảng pf_forms dưới vai r_forms — vai KN không có quyền; cần hàm SECURITY DEFINER mới). Thay khóa giữ suốt bằng Ý ĐỊNH HỦY bền: bảng trạng thái `pf_extension.khtc_cancel_intent` (migration pf `20261025140626_pf_extension_khtc_cancel_intent`, RLS tenant, không DELETE). `GuardCancel` → `CancelGuard` (khtcreport): `BeginCancel` (giao dịch ngắn, khóa `khtc_snapshot`, chưa Bản cuối → ghi ý định hạn 2 phút giờ DB) → cap/forms `CancelPeriod` ngoài giao dịch → `FinishCancel` (giao dịch ngắn, khóa: sự kiện 'cancelled' + bỏ ý định). cap/forms lỗi hoặc ghi sự kiện lỗi → `AbortCancel`. Đọc kỳ, Bản cuối, kiểm điều kiện trước, ngoài giao dịch | `khtcperiod/app/progress.go` `Cancel`, `khtcreport/app/service.go` | — |
| Ý định hủy chặn gì | Trigger (cùng khóa tư vấn nên tuần tự với Begin/Finish): Bản cuối (`khtc_report_final`) và đóng kỳ ('closed') bị từ chối khi ý định còn hạn — đúng những gì khóa cũ chặn. Mã lỗi mới 409 `extension.khtc_period_cancelling` (DB `khtc_report_cancelling`, `khtc_period_cancelling`); hủy lần hai đồng thời cũng nhận mã này. Nháp/xuất bản Excel không bị chặn (khóa cũ cũng chỉ xếp hàng chúng) | `fn_khtc_report_guard`, `fn_khtc_report_period_guard` (CREATE OR REPLACE, thân như 20261015223036 + một điều kiện) | — |
| Hủy kỳ: tiến trình chết giữa chừng | Hết hạn ý định (2 phút) thì Bản cuối/đóng kỳ chạy lại được. cap/forms có thể đã hủy trong khi sổ KHTC chưa — "Hủy kỳ" lần nữa hoàn tất (lỗi "đã hủy" của cap/forms được bỏ qua như trước). Hiếm (đường Admin) | — | Job đối soát nếu cần |
| Hạn dùng giờ nào | Ý định hủy KHTC: giờ DB (`now()`, trigger so cùng giờ). Nhận việc email lịch: đồng hồ ứng dụng (như `notify` #126, test dùng đồng hồ cố định) | — | — |
| Còn lại, ngoài phạm vi gói này | Consumer khác cũng gọi ra ngoài khi giao dịch inbox còn mở: `notify/app/deliver.go` (`Pusher.Send`, giao dịch riêng trong lúc giữ giao dịch inbox), `notify/app/email.go` (`Emails.EmailOf`, SMTP), consumer `pf/accounts` (Keycloak Admin REST — `cmd/worker/accounts.go`), `cooperative/season/app/wiring.go` (`GpsLog.RecordFix`, HTX). Với cơ chế mới sửa bằng cách bọc lời gọi trong `txdeps.Get` | — | Gói sau (làn shared/HTX) |

Bằng chứng (PostgreSQL thật, cụm riêng `scripts/pg-ephemeral.ps1`). Test ĐỎ trên dev 9fb647e9 (chạy với mã dev, chỉ thêm test),
XANH sau sửa:

| Test | Trên dev | Sau sửa |
|---|---|---|
| `apiclient/public` `TestExportOutsideInboxTxOnSingleConnPool_P0PoolShared2` (pool inbox cỡ 1, `Put` treo) | đỏ: `Put` khi giữ 1 kết nối | đạt |
| `mrv/handler` `TestExportPutOutsideInboxTx_P0PoolShared2` | đỏ: `Put` khi giữ 1 kết nối | đạt |
| `tests/reports` `TestP0PoolShared2_ExportPutOutsideInboxTx` | đỏ: `Put` khi giữ 1 kết nối | đạt |
| `tests/reports` `TestP0PoolShared2_DeliverySMTPOutsideInboxTx` | đỏ: SMTP khi giữ 1 kết nối | đạt |
| `tests/reports` `TestP0PoolShared2_DeliverySMTPErrorReleasesClaim`, `…DeliveryConcurrentRedeliverySendsOnce` | đỏ (chưa có cột nhận việc); với mã dev + migration mới thì đạt — đây là test ngữ nghĩa, không phải test tái hiện | đạt |
| kn77 `TestP0PoolShared2_DispatchSendOutsideInboxTx` (pool chung cỡ 2) | đỏ: SMTP khi giữ 1 kết nối | đạt |
| kn77 `TestP0PoolShared2_KHTCCancelNoNestedTx` (4 hủy song song, pool chung cỡ 2, hạn 10 s) | đỏ: 4/4 treo tới hết hạn | đạt |
| `cntsalert/handler` `TestCNTSAlertPortsOutsideInboxTx_P0PoolShared2` | đỏ: port gọi khi giữ 1 kết nối | đạt |
| `extension/handler` `TestSupportRequestGpsLogOutsideInboxTx_P0PoolShared2` | đỏ: `RecordFix` khi giữ 1 kết nối | đạt |
| `shared/inbox` `TestProcessRunsExternalIOOutsideInboxTx` (port mở giao dịch trên CÙNG pool cỡ 1) | đỏ: hết hạn 15 s (khóa chết pool cỡ 1) | đạt |
| kn77 `TestP0PoolShared2_DispatchLinkStoreGetOutsideTx` (liên kết tải A4, pool chung cỡ 2, `Store.Get` treo) | đỏ: `Store.Get` khi giữ 1 kết nối | đạt |
| kn77 `TestP0PoolShared2_KHTCCancelIntentBlocksFinal` (ý định hủy còn hạn chặn Bản cuối + hủy lần hai 409 `extension.khtc_period_cancelling`; hết hạn → Bản cuối chạy được, hủy bị từ chối) | đỏ (chưa có ý định hủy) | đạt |

Kiểm lại 26/10/2026 (phiên Claude thứ hai, dev 5396bb64): chạy lại các test tái hiện với mã sản phẩm của dev (bản sao tạm, giữ
migration + hàm store mới, chỉ đổi tệp nguồn của các điểm sửa về dev) — tất cả đỏ như bảng; bổ sung hai test kn77 cuối bảng (liên
kết tải chưa có test tái hiện; ngữ nghĩa ý định hủy chưa có test xác định). Bộ đầy đủ xanh trên nhánh.
