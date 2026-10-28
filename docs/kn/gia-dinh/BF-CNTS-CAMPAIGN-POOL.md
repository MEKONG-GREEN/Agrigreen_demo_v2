## BF-CNTS-CAMPAIGN-POOL — đợt điều tra CNTS giữ giao dịch rồi gọi cap/forms trên cùng pool (#156, hậu kiểm #27 W11.50)

Lỗi: `cntscampaign/app/lifecycle.go` `change` mở giao dịch pf_extension, giữ khóa tư vấn `cnts_campaign:<tenant>:<đợt>`, rồi
TRONG giao dịch gọi cap/forms (`LatestTemplate`, `PublishBatch`, `ChangeDue`, `ClosePeriod`, `ReopenPeriod`, `Progress` — mỗi
lời gọi một giao dịch riêng trên CÙNG pool app_platform, ADR-0028). Pool chỉ còn một kết nối trống → giao dịch ngoài chiếm
kết nối đó, lời gọi cap/forms chờ kết nối thứ hai tới hạn → 500. Cả năm thao tác (phát hành, nhắc, gia hạn, đóng, mở lại) đi
qua `change`. Sửa theo đúng mẫu ý định hủy kỳ KHTC (P0-POOL-SHARED-2, #143).

| Câu hỏi / mục | Lựa chọn đã dùng | Ở đâu | Đổi thế nào |
|---|---|---|---|
| Thay khóa giữ suốt bằng gì | Ý ĐỊNH THAO TÁC bền: bảng trạng thái `pf_extension.cnts_campaign_intent` (một dòng/đợt, RLS tenant, không DELETE, migration pf `20261028081435`). Bước 1 giao dịch ngắn (khóa đợt): idempotency, thấy đợt, trạng thái, ghi ý định (token, thao tác, hạn giờ DB) → commit. Bước 2 cap/forms NGOÀI giao dịch. Bước 3 giao dịch ngắn (khóa): bỏ ý định của token, kiểm lại trạng thái/phiên bản/số sự kiện, ghi sự kiện đợt (+ outbox nhắc nộp) cùng giao dịch | `app/lifecycle.go` `change`, `store.Claim/Release` | — |
| Ý định chặn gì | Trigger (cùng khóa tư vấn nên tuần tự với bước 1/3): sửa nháp (phiên bản mới) và MỌI sự kiện đợt khi còn ý định còn hạn — đúng những gì khóa cũ chặn. Bước 3 bỏ ý định của mình trước khi ghi sự kiện trong cùng giao dịch. Thao tác thứ hai (kể cả cùng thao tác, khóa idempotent khác) → 409 `extension.cnts_campaign_busy`, KHÔNG gọi cap/forms | `fn_cnts_campaign_draft`, `fn_cnts_campaign_transition` (CREATE OR REPLACE, thân như 20261024091058 + một điều kiện) | — |
| Gọi lặp cùng Idempotency-Key | Như cũ: sự kiện đã ghi với khóa → trả đợt hiện tại, không gọi cap/forms | `change` bước 1 | — |
| Hạn lời gọi cap/forms | = thời điểm TRƯỚC giao dịch bước 1 + hạn ý định − biên (¼ hạn, tối đa 15 s), như #143 C1. Hạn ý định mặc định 2 phút (`DefaultLease`; kiểm thử `UseCNTSCampaignLease`). Quá hạn → 503 `extension.dependency_unavailable`, ý định trả ngay | `change` | Đổi `DefaultLease` |
| cap/forms lỗi | Trả ý định ngay (giao dịch riêng, ctx không hủy), lỗi của cap/forms trả nguyên như trước. Lời gọi bị cắt (hạn/hủy) với thao tác ghi kỳ native → không rõ kết quả: đánh dấu lệch rồi đối soát (dòng dưới) | `wrapUp` | — |
| Ghi sự kiện lỗi (bước 3) | Đánh dấu lệch rồi giữ lại ý định (của mình còn hạn, hoặc nhận lại nếu không ai giữ): đợt còn Nháp → bù phát hành (hủy kỳ native vừa tạo, `CancelPeriod`, khóa `…:compensate`); rồi đối soát kỳ native theo sổ đợt — gia hạn/đóng/mở lại nay CÓ bù (bản cũ không). Người khác đang giữ đợt → không bù, không đối soát (khóa `PublishBatch` cố định theo đợt: kỳ native đó cũng là của người đang phát hành); người giữ đối soát khi ghi xong | `wrapUp`, `Publish` | — |
| Mất ý định trước bước 3 (giao dịch bước 3 chờ lâu quá hạn) | Không ai giữ → nhận lại ý định trong giao dịch bước 3 rồi kiểm đợt không đổi (ai chen vào thì phiên bản/sự kiện khác → 409 `extension.cnts_campaign_state` + bù/đối soát). Người khác đang giữ → 409 `extension.cnts_campaign_interrupted`, không ghi sự kiện, chỉ đánh dấu lệch | `change` bước 3 | — |
| Yêu cầu cũ ghi đè kỳ native sau yêu cầu kế tiếp (review Codex [P2]: A gia hạn 12/10 mất ý định ngay trước `ChangeDue`, B gia hạn 13/10 xong, rồi `ChangeDue` của A chạy) | cap/forms CHƯA có ghi có điều kiện (CAS/fence) — ticket con lane:shared #159. Ở lane:kn: **đối soát sau ghi** theo sổ đợt. `cnts_campaign_intent` thêm `stale_gen`/`synced_gen`. Yêu cầu biết mình mất ý định (hoặc lời gọi không rõ kết quả, hoặc ghi sự kiện lỗi) tăng `stale_gen` SAU khi lời gọi cap/forms đã trả về. Người giữ ý định khi `stale_gen > synced_gen` (chính yêu cầu đó nếu nhận lại được; không thì người đang giữ khi ghi xong; hoặc thao tác kế tiếp) chạy `settle`: đọc sổ đợt + `stale_gen` (đang giữ ý định nên sổ đợt không đổi), `Progress` kỳ native, rồi Đang thu thập → mở lại nếu kỳ chốt + `ChangeDue` về hạn của sổ; Đã đóng → `ClosePeriod` nếu kỳ đang nhận bài; cuối vòng `stale_gen` không đổi → `synced_gen = gen`, bỏ ý định; đổi → vòng sau (tối đa 3); mất ý định giữa vòng → đánh dấu lệch, nhận lại hoặc để người giữ. Không bao giờ ghi lại giá trị của yêu cầu cũ — luôn ghi theo sổ đợt cam kết mới nhất | `change`, `wrapUp`, `settle`, `reconcile`, `store.MarkStale/StaleGen/MarkSynced/Renew` | Có port CAS/fence (#159) → truyền vào lời gọi, giữ đối soát làm lớp phòng thủ |
| Bảo đảm và giới hạn | Sau mọi xen kẽ mà tiến trình yêu cầu cũ còn sống tới lúc đánh dấu (gồm mọi nhánh mất ý định đã hỗ trợ): khi không còn thao tác dở dang, kỳ native (trạng thái + hạn hiệu lực) = sự kiện đợt đã cam kết mới nhất. Chưa phủ (chờ #159): tiến trình chết ngay sau khi lời gọi cũ commit mà chưa đánh dấu; commit ở pf_forms tới SAU khi lời gọi đã bị hủy và đã đối soát; kỳ đã chốt mang hạn ghi trước khi chốt (pf_forms từ chối `ChangeDue` trên kỳ chốt — đối soát chỉ so trạng thái với đợt Đã đóng) | — | #159 |
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

Review Codex [P2] (yêu cầu cũ ghi đè kỳ native): chạy test mới với `lifecycle.go` của `fd024558` (trước sửa) → cả 5 đỏ; sau sửa
đạt. Mỗi test kiểm sau cả hai yêu cầu: kỳ native (trạng thái + hạn hiệu lực `pf_forms.effective_due`) = sổ đợt, không còn ý
định giữ, không còn dấu lệch. Overlay `TestReviewer156StaleExtendMustNotOverwriteSuccessor` của Codex: đạt.

| Test | Xen kẽ | Trước sửa | Sau sửa |
|---|---|---|---|
| `…StaleExtendDoesNotOverwriteSuccessor` | đúng xen kẽ Codex: A gia hạn 12/10 mất ý định trước `ChangeDue`, B gia hạn 13/10 xong, rồi A ghi | đỏ: native 12/10, đợt 13/10 | A 409 state; native 13/10 |
| `…StaleLaterExtendDoesNotOverwriteSuccessor` | như trên, A gia hạn 20/10 (đối soát phải lùi hạn) | đỏ: native 20/10 | native 13/10 |
| `…StaleWriteWhileSuccessorHolds` | A ghi native SAU B khi B còn giữ ý định; A trả kết quả trước khi B ghi sự kiện | đỏ: không có dấu lệch | A 409 interrupted + dấu lệch; B ghi xong đối soát → native 13/10 |
| `…StaleCloseDoesNotOverwriteSuccessor` | A đóng mất ý định trước `ClosePeriod`, B gia hạn xong, rồi A chốt kỳ | đỏ: đợt Đang thu thập, kỳ chốt | kỳ mở lại, hạn 13/10 |
| `…StaleReopenDoesNotOverwriteSuccessor` | đợt Đã đóng; A mở lại mất ý định trước `ReopenPeriod`, B mở lại + C đóng xong, rồi A mở kỳ | đỏ: đợt Đã đóng, kỳ reopened | kỳ chốt |
