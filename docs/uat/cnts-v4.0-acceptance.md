# Nghiệm thu CNTS v4.0 (BRD 2.0, CR-CNTS-001)

Căn cứ duy nhất: BRD CNTS v4.0. Mỗi gói CNTS v4.0 thêm phần FN mình chạm. Kiểm thử tích hợp chạy trên PostgreSQL thật
(pg-ephemeral); camera, GPS của Mobile là nguồn giả trong kiểm thử.

## FN-21 — Chiến dịch điều tra mở rộng: người phụ trách, sao chép đợt, Tạo chiến dịch (CNTS-CAMPAIGN-SCOPE #236)

Giả định: [CNTS-CAMPAIGN-SCOPE](../kn/gia-dinh/CNTS-CAMPAIGN-SCOPE.md); nền W11.50 ([cnts-v3.7-acceptance.md](cnts-v3.7-acceptance.md) mục FN-21).

| Tiêu chí BRD v4.0 | Bằng chứng tự động | Trạng thái |
|---|---|---|
| B1 "+ Tạo đợt điều tra": người phụ trách bắt buộc, là cán bộ Cấp Tỉnh CNTS của tỉnh (cán bộ Cấp Xã, mã lạ → 422) | `TestCNTSCampaignOwnerAndCopy_236` (B1), `TestDraftValidate`; Vitest `CampaignScreen.test.tsx` "Tạo chiến dịch", `campaign.test.ts` #236 | Đạt |
| Ma trận quyền FN-21: cán bộ tổng hợp CRU (tạo, phát hành, theo dõi, đóng); viên chức, chủ nhiệm CRU khi là người phụ trách; Lãnh đạo phòng xem + mở lại; người không được giao bị từ chối 403 khi sửa, phát hành, nhắc, gia hạn, đóng | `TestCNTSCampaignOwnerAndCopy_236` ("Người không phụ trách đợt bị từ chối 403…") | Đạt |
| Luồng phụ "sao chép đợt cũ": đợt Nháp mới mang biểu, phạm vi, người phụ trách; không mang số liệu (kỳ native riêng, 0 bài, kết quả rỗng) | `TestCNTSCampaignOwnerAndCopy_236` ("Sao chép đợt cũ…"); Vitest `CampaignScreen.test.tsx` "Sao chép đợt cũ" | Đạt |
| Menu "Chiến dịch điều tra › Tạo chiến dịch" chạy thật (không còn "Sắp có") | `kn-menu.contract.test.ts` (UP-10 CNTS Cấp Tỉnh), `TestBuilderMatchesCompatibilitySnapshot`, `TestWebMenuFixture` | Đạt |
| B2–B5, BR-01…BR-04, AC-01…AC-04 (phát hành, tiến độ theo Khu vực, đóng đợt → Không nộp, tách lũy kế BM-04) | `TestCNTSCampaign_LifecycleSeparateFromCumulative_W1150`, `TestBFCNTSCampaignCAS_*`, `TestBFCNTSCampaignPool_*` | Đạt (W11.50, #201) |
| B1 phạm vi "hộ" (chọn hộ chăn nuôi `active` — ADR-0056) | — | Gói #258 (sau #251) |

Cách chạy:

```powershell
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v -run 'CNTSCampaign' ./tests/kn77/
go test ./internal/kn/extension/cntscampaign/... ./internal/kn/extension/menu/...
cd apps/web-ops; npx vitest run src/kn/cnts/campaign/ src/api/mock/kn-menu.contract.test.ts
```
