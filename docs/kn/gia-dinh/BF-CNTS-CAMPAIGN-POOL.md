## BF-CNTS-CAMPAIGN-POOL — đợt điều tra CNTS giữ giao dịch rồi gọi cap/forms trên cùng pool (#156, hậu kiểm #27 W11.50)

Lỗi: `cntscampaign/app/lifecycle.go` `change` mở giao dịch pf_extension, giữ khóa tư vấn `cnts_campaign:<tenant>:<đợt>`, rồi
TRONG giao dịch gọi cap/forms (`LatestTemplate`, `PublishBatch`, `ChangeDue`, `ClosePeriod`, `ReopenPeriod`, `Progress` — mỗi
lời gọi một giao dịch riêng trên CÙNG pool app_platform, ADR-0028). Pool chỉ còn một kết nối trống → giao dịch ngoài chiếm
kết nối đó, lời gọi cap/forms chờ kết nối thứ hai tới hạn → 500. Cả năm thao tác (phát hành, nhắc, gia hạn, đóng, mở lại) đi
qua `change`. Sửa theo đúng mẫu ý định hủy kỳ KHTC (P0-POOL-SHARED-2, #143).

| Câu hỏi / mục | Lựa chọn đã dùng | Ở đâu | Đổi thế nào |
|---|---|---|---|
| Thay khóa giữ suốt bằng gì | Ý ĐỊNH THAO TÁC bền: bảng trạng thái `pf_extension.cnts_campaign_intent` (một dòng/đợt, RLS tenant, không DELETE, migration pf `20261028034338`). Bước 1 giao dịch ngắn (khóa đợt): idempotency, thấy đợt, trạng thái, ghi ý định (token, thao tác, hạn giờ DB) → commit. Bước 2 cap/forms NGOÀI giao dịch. Bước 3 giao dịch ngắn (khóa): bỏ ý định của token, kiểm lại trạng thái/phiên bản/số sự kiện, ghi sự kiện đợt (+ outbox nhắc nộp) cùng giao dịch | `app/lifecycle.go` `change`, `store.Claim/Release` | — |
| Ý định chặn gì | Trigger (cùng khóa tư vấn nên tuần tự với bước 1/3): sửa nháp (phiên bản mới) và MỌI sự kiện đợt khi còn ý định còn hạn — đúng những gì khóa cũ chặn. Bước 3 bỏ ý định của mình trước khi ghi sự kiện trong cùng giao dịch. Thao tác thứ hai (kể cả cùng thao tác, khóa idempotent khác) → 409 `extension.cnts_campaign_busy`, KHÔNG gọi cap/forms | `fn_cnts_campaign_draft`, `fn_cnts_campaign_transition` (CREATE OR REPLACE, thân như 20261024091058 + một điều kiện) | — |
| Gọi lặp cùng Idempotency-Key | Như cũ: sự kiện đã ghi với khóa → trả đợt hiện tại, không gọi cap/forms | `change` bước 1 | — |
| Hạn lời gọi cap/forms | = thời điểm TRƯỚC giao dịch bước 1 + hạn ý định − biên (¼ hạn, tối đa 15 s), như #143 C1. Hạn ý định mặc định 2 phút (`DefaultLease`; kiểm thử `UseCNTSCampaignLease`). Quá hạn → 503 `extension.dependency_unavailable`, ý định trả ngay | `change` | Đổi `DefaultLease` |
| cap/forms lỗi | Trả ý định ngay (giao dịch riêng, ctx không hủy), lỗi của cap/forms trả nguyên như trước | `release` | — |
| Ghi sự kiện lỗi (bước 3) | Bù như cũ: phát hành → hủy kỳ native vừa tạo (`CancelPeriod`, khóa `…:compensate`). Khóa idempotent của `PublishBatch` cố định theo đợt nên kỳ native đó chỉ của lần phát hành này — hủy an toàn. Gia hạn/đóng/mở lại: không có bù (như bản cũ). Rồi trả ý định | `change`, `Publish` | — |
| Mất ý định trước bước 3 (giao dịch bước 3 chờ lâu quá hạn) | Không ai giữ → nhận lại ý định trong giao dịch bước 3 rồi kiểm đợt không đổi (ai chen vào — vd sửa nháp — thì phiên bản/sự kiện khác → 409 `extension.cnts_campaign_state` + bù). Người khác đang giữ → 409 `extension.cnts_campaign_interrupted`, không ghi gì (cap/forms đã đổi: gia hạn/đóng/mở lại chưa có bù — như bản cũ khi ghi sự kiện lỗi) | `change` bước 3 | Job đối soát đợt ↔ kỳ native nếu cần |
| Nhắc xã chưa nộp | Tiến độ đọc MỘT lần ngoài giao dịch; sự kiện "reminded" và các sự kiện nhắc (outbox) ghi theo đúng ảnh tiến độ đó (bản cũ đọc tiến độ hai lần, cả hai trong giao dịch) | `Remind`, `emitReminders` | — |
| Quyền | Không đổi: `writer` (Cấp Tỉnh CNTS, không chỉ xem), mở lại cần quyền `mo_lai_ky`; cap/forms vẫn kiểm `publish_by`/`close_by` của Nút phát hành | `service.go` | — |
| Tiến trình chết giữa chừng | Ý định hết hạn (2 phút) thì thao tác/sửa nháp khác chạy được. Phát hành: cap/forms có thể đã tạo kỳ native mà đợt chưa ghi — gọi lại `PublishBatch` cùng khóa đợt trả 409 xung đột (hành vi có sẵn từ W11.50, ngoài phạm vi gói này) | — | Port cap/forms tra kỳ theo khóa idempotent (lane:shared) nếu cần |

Bằng chứng (PostgreSQL thật, cụm riêng `scripts/pg-ephemeral.ps1`, Go 1.26.10). Đỏ: chạy test mới với `lifecycle.go` của dev
(bản tạm, chỉ thêm hàm đọc `current`):

| Test (`tests/kn77/cnts_campaign_pool_integration_test.go`) | Trên dev | Sau sửa |
|---|---|---|
| `TestBFCNTSCampaignPool_TransitionsWithOneFreeConn` (chuyển thể `TestReviewerCampaignPublishPoolBoundary` của Codex; phát hành → nhắc → gia hạn → đóng → mở lại, mỗi bước pool chỉ còn 1 kết nối trống) | đỏ: phát hành 500 sau 15 s | đạt |
| `TestBFCNTSCampaignPool_ConcurrentOnSmallSharedPool` (pool chung cỡ 2; 3 đợt × 2 phát hành song song; rồi đóng + gia hạn song song) | đỏ: 6/6 treo tới hạn 10 s | đạt; mỗi đợt đúng một sự kiện phát hành, trạng thái đợt khớp kỳ native |
| `TestBFCNTSCampaignPool_IntentBlocksOtherWriters`, `…FormsDeadlineReleasesIntent`, `…LostIntent`, `…PublishCompensatesOnRecordFailure` | test ngữ nghĩa ý định | đạt |
