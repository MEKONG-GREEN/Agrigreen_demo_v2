# Nghiệm thu CNTS v4.0 (BRD 2.0, CR-CNTS-001)

Căn cứ duy nhất: BRD CNTS v4.0. Mỗi gói CNTS v4.0 thêm phần FN mình chạm. Kiểm thử tích hợp chạy trên PostgreSQL thật
(pg-ephemeral); camera, GPS của Mobile là nguồn giả trong kiểm thử.

## FN-12 — Lịch công tác: check-in, check-out kèm ảnh (CNTS-V4-CHECKOUT-PHOTO #249)

Giả định và mô hình dữ liệu: [CNTS-V4-CHECKOUT-PHOTO](../kn/gia-dinh/CNTS-V4-CHECKOUT-PHOTO.md).

| Tiêu chí BRD v4.0 | Bằng chứng tự động | Trạng thái |
|---|---|---|
| CV-07, luồng ngoại lệ "Check-in, check-out không có ảnh → chặn": check-in thiếu ảnh, ảnh hỏng, không phải ảnh, > 500 KB bị từ chối 422 trường `photo` | `TestWorkScheduleCheckoutPhoto`, `TestVisitPhotoType`; field-app `hủy chụp ảnh → không check-in` | Đạt |
| B4b: Check-out chỉ sau check-in; nút Check-out chỉ hiện sau check-in | `TestWorkScheduleCheckoutPhoto` (409 `extension.checkout_no_checkin`); field-app `CV-08: nút Check-out chỉ sau check-in` | Đạt |
| AC-09: bấm check-out chưa chụp ảnh thì không lưu được | `TestWorkScheduleCheckoutPhoto` | Đạt |
| BR-06: check-in, check-out lưu ảnh, GPS, thời điểm, độ chính xác; không sửa được | `TestWorkScheduleCheckoutPhoto` (check-out lần hai 409 `extension.checkout_exists`; ảnh tải lại đúng nội dung), `TestWorkScheduleCheckin` | Đạt |
| Check-out cách địa điểm CV-06 > 500 m → cảnh báo | `TestWorkScheduleCheckoutPhoto` (`checkout_far` ở phản hồi và chi tiết lịch) | Đạt |
| AC-11: check-out mất mạng 11:30, có mạng 14:00 → ghi 11:30 | `TestWorkScheduleCheckoutPhoto` (giờ thiết bị giữ nguyên khi gửi muộn); field-app lệnh chờ giữ `captured_at` lúc bấm | Đạt |
| AC-12: check-in 08:00, check-out 11:30 → thời gian ở địa điểm 3 giờ 30 phút | `TestWorkScheduleCheckoutPhoto` (`stay_minutes` 210), `TestCheckoutRules`; web `visits.test.tsx`; field-app `máy chủ trả check-out` | Đạt |
| AC-13: check-out offline 16:30 ngày 06/10, đồng bộ 07:10 ngày 07/10 → ghi 16:30 kèm "Đồng bộ trễ" | `TestWorkScheduleCheckoutPhoto` (`synced_late`, `synced_at`), `TestCheckoutRules`; web hiện "Đồng bộ trễ · thời điểm đồng bộ" | Đạt phần ghi nhận; gỡ nhãn, tự đóng cảnh báo: #250 |
| UR-TINH-18: người theo dõi xem check-in, check-out, thời gian ở địa điểm và ảnh | `TestWorkScheduleCheckoutPhoto` (người duyệt tải ảnh; cán bộ xã khác không); web chi tiết lịch khối "Check-in, check-out tại địa điểm" | Đạt ở chi tiết lịch; màn theo dõi Hình 49: #250 |
| Dữ liệu cũ: check-in trước v4.0 không ảnh giữ nguyên, đánh dấu, không bắt nhập bù; đếm trước/sau, quay lui | `TestWorkScheduleCheckinLegacy` (số dòng, băm dòng cũ không đổi; `verify_chain` đúng; câu INSERT của bản trước vẫn chạy) | Đạt |
| BR-07/AC-10: nhãn "Chưa check-out" lúc 23:59, cảnh báo người theo dõi | — | Gói CN-4 #250 |

Cách chạy:

```powershell
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v -run 'WorkSchedule' ./internal/kn/extension/cntscal/handler/
go test ./internal/kn/extension/domain/ -run 'VisitPhoto|CheckoutRules'
cd apps/web-ops; npx vitest run src/kn/schedule/
cd apps/field-app; flutter test test/work_schedule_checkin_test.dart
```

## FN-12/FN-13 — Theo dõi check-in, check-out; cảnh báo "Chưa check-out" (CNTS-V4-CHECKIN-MONITOR #250)

Giả định: [CNTS-V4-CHECKIN-MONITOR](../kn/gia-dinh/CNTS-V4-CHECKIN-MONITOR.md). Giờ giả lập (đồng hồ cố định của kiểm thử), mốc 23:59
tính theo giờ Việt Nam.

| Tiêu chí BRD v4.0 | Bằng chứng tự động | Trạng thái |
|---|---|---|
| FN-12 BR-07: mục đã check-in, tới 23:59 cùng ngày chưa check-out → nhãn "Chưa check-out" ở màn cán bộ xã và màn theo dõi | `TestNoCheckout` (23:58:59 chưa, 23:59 có, mốc 16:59 UTC); `TestWorkScheduleNoCheckout` (`no_checkout` ở chi tiết lịch, `mine` của cán bộ xã, danh sách theo dõi `no_checkout_count`, cảnh báo `checkout_missing`); web `visits.test.tsx`; field-app `CNTS v4.0 BR-07 (#250)` | Đạt |
| FN-12 AC-10: check-in 08:00, chưa check-out đến 23:59 → sáng hôm sau người theo dõi thấy nhãn | `TestWorkScheduleNoCheckout` (người duyệt đọc lịch lúc 23:59 và sau đó) | Đạt |
| Mục III.5 dòng 13 / FN-13 BR-04: cảnh báo "Chưa check-out", mức Cần chú ý, gửi người theo dõi lịch cán bộ xã; không tạo trùng | `TestWorkScheduleNoCheckout` (job `extension_cnts_alert` sinh 2 cảnh báo, người nhận = người duyệt được lịch; chạy lại không sinh thêm), `TestTransition` (bảng loại dòng 13) | Đạt |
| FN-12 BR-08 / AC-13: check-out offline 16:30, đồng bộ 07:10 hôm sau → gỡ nhãn, cảnh báo tự đóng, lịch ghi 16:30 kèm "Đồng bộ trễ" | `TestWorkScheduleNoCheckout` (nhãn gỡ ngay khi đọc; job tự đóng cảnh báo, `auto_closed`, kết quả ghi giờ check-out và giờ đồng bộ) | Đạt (tự đóng trong ≤ 5 phút — chu kỳ job) |
| UR-TINH-18, Hình 49: màn theo dõi check-in, check-out, thời gian ở địa điểm, ảnh của cán bộ xã; mục chưa check-out hiện nhãn | web `visits.test.tsx` (`<VisitMonitor>`: cột, Đồng bộ trễ, lọc "Chỉ mục Chưa check-out", xem ảnh), `schedule.test.tsx` (tab "Check-in, check-out") | Đạt |
| Job tìm tenant có check-in bằng vai `ag_ops_jobs` (chỉ cột `tenant_id`, `day`) | `TestWorkScheduleNoCheckout` (`CNTSAlertTenants`) | Đạt |
| Dữ liệu cũ: check-in trước v4.0 (không ảnh) không gắn nhãn, không sinh cảnh báo — không bắt nhập bù | `TestWorkScheduleNoCheckout` | Đạt |

Cách chạy:

```powershell
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v -run 'WorkScheduleNoCheckout' ./internal/kn/extension/cntscal/handler/
go test ./internal/kn/extension/domain/ ./internal/kn/extension/cntsalert/domain/
cd apps/web-ops; npx vitest run src/kn/schedule/
cd apps/field-app; flutter test test/work_schedule_checkin_test.dart
```
# Nghiệm thu CNTS v4.0 (BRD 2.0)

Đối chiếu theo BRD CNTS v4.0 (CR-CNTS-001). Mỗi gói thêm mục của FN mình chạm; kiểm thử tích hợp chạy trên PostgreSQL thật
(pg-ephemeral), kho định danh, danh mục, đơn vị hành chính là bản giả trừ khi ghi khác.

## FN-10 — Danh sách hộ chăn nuôi (CNTS-V4-HOUSEHOLD-LIST #251)

Nguồn và giới hạn: [giả định](../kn/gia-dinh/CNTS-V4-HOUSEHOLD-LIST.md), [ADR-0056](../adr/0056-ho-chan-nuoi-cnts-v4.md).

| Tiêu chí BRD | Bằng chứng tự động | Phạm vi |
|---|---|---|
| H1–H3: thêm hộ HCN-02…HCN-06 + check-in tại nhà hộ → Đang dùng; mã HCN-01 tự cấp `HCN`+7 số, không dùng lại, theo tenant | `TestLivestockHouseholds_251`, `TestCodeNameChanged` | G1 |
| AC-05: thêm hộ chưa check-in → không lưu (422 `checkin_required`) | `TestLivestockHouseholds_251`; vitest `livestock.test.ts` | G1 |
| AC-06: Cấp Xã chỉ thấy, chỉ thêm hộ xã mình | `TestLivestockHouseholds_251` | G1 |
| Ma trận quyền: cán bộ tổng hợp Cấp Tỉnh CRU + vô hiệu hóa; viên chức Cấp Tỉnh chỉ xem; Cấp TW không có quyền | `TestLivestockHouseholds_251` | G1 |
| HCN-02/03 dữ liệu cá nhân: họ tên, SĐT ở kho định danh (không ở sổ KN); danh sách che bớt, SĐT che một phần (Mục I.8.1) | `TestLivestockHouseholds_251`, `TestIdentityRevisions_251` | G1 |
| HCN-05: vật nuôi theo danh mục FN-06 đang dùng, số nguyên ≥ 0, nhiều vật nuôi; số rất lớn (1e100000) bị chặn | `TestLivestockHouseholds_251`, `TestNormalizeFields`, `TestAccuracyAndCheckin` | G1 |
| Ngoại lệ: trùng họ tên, ấp, xã với hộ Đang dùng → cảnh báo (409 kèm mã hộ trùng); xác nhận hộ khác → lưu | `TestLivestockHouseholds_251` | G1 |
| Ngoại lệ: sai số GPS > 50 m → cảnh báo, vẫn lưu; không bắt được GPS → không check-in, báo thử lại | `TestLivestockHouseholds_251`; vitest `livestock.test.ts` | G1 |
| BR-03: vị trí chỉ từ check-in GPS, không nhập tay; sửa thông tin không đổi vị trí | `TestLivestockHouseholds_251` | G1 |
| H4: đổi vị trí phải check-in lại và nhập lý do | `TestLivestockHouseholds_251` | G1 |
| BR-05, AC-08, HCN-09 (Q9 chốt 08/11/2026): lịch sử người sửa, thời điểm, cũ → mới; họ tên đầy đủ qua đúng bản sửa vault có audit, chỉ SĐT che | `TestLivestockHouseholds_251`, `TestIdentityRevisions_251`, `TestResolveRevisionNames_276`, `TestLivestockHistoryVersionsAndUnavailable_276`, `TestHCN09NativeCurrentSession_276`; web `history.test.tsx` | Bằng chứng cục bộ #276; native PG/vault và UI/mock tách biệt |
| BR-04: không xóa, chỉ vô hiệu hóa (cán bộ tổng hợp Cấp Tỉnh, có lý do); không sửa/kích hoạt lại sau vô hiệu hóa | `TestLivestockHouseholds_251` | G1 |
| BR-08, AC-09 (phần danh sách): hộ Chờ bổ sung vị trí → cán bộ xã check-in → Đang dùng, lịch sử ghi chuyển trạng thái | `TestLivestockHouseholds_251` (hộ chờ ghi như BM-10 sẽ ghi) | G1; tạo hộ từ BM-10: #252 |
| BR-07, AC-07 (phần danh sách): hộ vô hiệu hóa không còn trong danh sách Đang dùng | `TestLivestockHouseholds_251` (lọc `status=active`) | G1; ô chọn DB-04: #252 |
| Xóa định danh xóa luôn bản sửa họ tên/SĐT (RB-3) | `TestIdentityRevisions_251` | G1 |
| Menu Tỉnh "Số liệu cấp xã › Danh sách hộ chăn nuôi", Xã "Dữ liệu › Danh sách hộ chăn nuôi" | `TestWebMenuFixture`, `TestBuilderMatchesWebGoldenFixture` | G1 |

Bổ sung #276: [phạm vi, ánh xạ tệp → bước → FN và cách chạy](../kn/gia-dinh/CNTS-V4-HCN09.md). Kiểm quyền đọc HCN-09 của các cohort FN-10 (kể cả chỉ xem), thu hồi/đổi Nút/phòng/phạm vi trên request kế tiếp, MFA/quota, missing-ref, KMS/audit lỗi và pool 1. Fixture định danh tổng hợp + native owner không thay bằng chứng OIDC/KMS triển khai; browser/mock không phải chứng nhận native hoặc toàn FN/UAT.

Chưa nghiệm thu ở gói này: đếm hộ ở báo cáo định kỳ (FN-16 BR-07, CN-5), lớp trang trại bản đồ (FN-18, CN-6), ô chọn DB-04 và tạo
hộ từ BM-10 (FN-11, #252).

## FN-21 — Chiến dịch điều tra mở rộng: người phụ trách, sao chép đợt, Tạo chiến dịch (CNTS-CAMPAIGN-SCOPE #236)

Giả định: [CNTS-CAMPAIGN-SCOPE](../kn/gia-dinh/CNTS-CAMPAIGN-SCOPE.md); nền W11.50 ([cnts-v3.7-acceptance.md](cnts-v3.7-acceptance.md) mục FN-21).

| Tiêu chí BRD v4.0 | Bằng chứng tự động | Trạng thái |
|---|---|---|
| B1 "+ Tạo đợt điều tra": người phụ trách bắt buộc, là cán bộ Cấp Tỉnh CNTS của tỉnh (cán bộ Cấp Xã, mã lạ → 422) | `TestCNTSCampaignOwnerAndCopy_236` (B1), `TestDraftValidate`; Vitest `CampaignScreen.test.tsx` "Tạo chiến dịch", `campaign.test.ts` #236 | Đạt |
| Ma trận quyền FN-21: cán bộ tổng hợp CRU (tạo, phát hành, theo dõi, đóng); viên chức, chủ nhiệm CRU khi là người phụ trách; Lãnh đạo phòng xem + mở lại; người không được giao bị từ chối 403 khi sửa, phát hành, nhắc, gia hạn, đóng | `TestCNTSCampaignOwnerAndCopy_236` ("Người không phụ trách đợt bị từ chối 403…") | Đạt |
| Luồng phụ "sao chép đợt cũ": đợt Nháp mới mang biểu, phạm vi, người phụ trách; không mang số liệu (kỳ native riêng, 0 bài, kết quả rỗng) | `TestCNTSCampaignOwnerAndCopy_236` ("Sao chép đợt cũ…"); Vitest `CampaignScreen.test.tsx` "Sao chép đợt cũ" | Đạt |
| Menu "Chiến dịch điều tra › Tạo chiến dịch" chạy thật (không còn "Sắp có") | `kn-menu.contract.test.ts` (UP-10 CNTS Cấp Tỉnh), `TestBuilderMatchesCompatibilitySnapshot`, `TestWebMenuFixture` | Đạt |
| B2–B5, BR-01…BR-04, AC-01…AC-04 (phát hành, tiến độ theo Khu vực, đóng đợt → Không nộp, tách lũy kế BM-04) | `TestCNTSCampaign_LifecycleSeparateFromCumulative_W1150`, `TestBFCNTSCampaignCAS_*`, `TestBFCNTSCampaignPool_*` | Đạt (W11.50, #201) |
| B1 phạm vi "hộ": chỉ hộ chăn nuôi Đang dùng trong phạm vi Nút (hộ Chờ bổ sung vị trí, ngoài Nút, mã lạ → 422); mỗi hộ một việc của cán bộ xã chứa hộ; tiến độ, kết quả, Excel theo hộ; sao chép mang hộ, hộ vô hiệu hóa sau đó không phát hành được | `TestCNTSCampaignHouseholdScope_236`, `TestDraftValidate`; Vitest `CampaignScreen.test.tsx` "Phạm vi theo hộ", `campaign.test.ts` | Đạt |

Cách chạy:

```powershell
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v -run 'CNTSCampaign' ./tests/kn77/
go test ./internal/kn/extension/cntscampaign/... ./internal/kn/extension/menu/...
cd apps/web-ops; npx vitest run src/kn/cnts/campaign/ src/api/mock/kn-menu.contract.test.ts
```

## FN-16, FN-17 — BM-11 theo loại chăn nuôi, số hộ/trang trại trang chủ (CNTS-V4-BM11-TYPES #253)

Nguồn và giới hạn: [giả định](../kn/gia-dinh/CNTS-V4-BM11-TYPES.md), [ADR-0056](../adr/0056-ho-chan-nuoi-cnts-v4.md) mục 9.

| Tiêu chí BRD | Bằng chứng tự động | Phạm vi |
|---|---|---|
| FN-16 BR-07, UR-TINH-16 (Hình 48): báo cáo định kỳ có bảng theo loại chăn nuôi — số hộ, số trang trại, số con theo vật nuôi — đếm, cộng từ danh sách hộ chăn nuôi (hộ Đang dùng) lúc lập báo cáo | `TestCNTSBM11_W1145`, `TestBM11HerdsOf_AC05`; vitest `bm11.test.tsx` | G1 |
| FN-16 AC-05: xã A 12 hộ đang dùng (3 trang trại nhỏ, 1 vừa) → 12 hộ, 4 trang trại; nông hộ 8, nhỏ 3, vừa 1, công nghiệp 0 | `TestCNTSBM11_W1145`, `TestBM11HerdsOf_AC05`; vitest `bm11.test.tsx` | G1 |
| FN-10 BR-08 (phần báo cáo): hộ Chờ bổ sung vị trí, hộ Vô hiệu hóa không tính vào báo cáo định kỳ | `TestCNTSBM11_W1145` | G1 |
| FN-10 BR-06: hộ đổi loại sau khi lập không làm đổi báo cáo đã lập; "Làm mới số liệu" khi còn soạn đếm lại | `TestCNTSBM11_W1145` | G1 |
| FN-16 BR-07: KSGM (BM-01), nhập liệu tuần (BM-04), khai thác (BM-17) vẫn tổng hợp theo xã, không chia theo loại | `TestCNTSBM11_W1145` (số chăn nuôi, thủy sản không đổi) | G1 |
| UR-TINH-16 xuất Word: bảng theo loại chăn nuôi trong Word BM-11 (mẫu v2), mẫu v1 vẫn dựng được | `TestBM11WordTemplates`, `TestCNTSBM11_W1145` (Word) | G1 |
| FN-17 (phạm vi gói): trang chủ Phòng CNTS có thẻ số hộ chăn nuôi, số trang trại (hộ Đang dùng, loại hiện hành); bấm mở danh sách hộ; phòng khác không có | `TestFN17_LivestockCounts_253`; vitest `depthome.test.ts` | G1 |

Chưa nghiệm thu ở gói này: lớp trang trại bản đồ (FN-18, CN-6); FN-17 AC-01 (500 người dùng, < 5 giây trên 3G) cần đo tải trên
môi trường triển khai.
hộ từ BM-10 (FN-11, #252 — xem mục FN-11 dưới).

## FN-11 — Báo cáo dịch bệnh BM-10 nhiều hộ (CNTS-V4-OUTBREAK-MULTI #252)

Nguồn và giới hạn: [giả định](../kn/gia-dinh/CNTS-V4-OUTBREAK-MULTI.md), [ADR-0056](../adr/0056-ho-chan-nuoi-cnts-v4.md) mục 7–8.

| Tiêu chí BRD | Bằng chứng tự động | Phạm vi |
|---|---|---|
| BR-03, DB-04: một ổ dịch chọn được nhiều hộ trong danh sách hộ chăn nuôi; hộ ngoài danh sách, xã khác, lặp → chặn | `TestFN11_MultiHousehold_252`, `TestCheckHouseholds`; vitest `outbreak-ui.test.tsx` | G1 |
| UR-XA-10: DB-04 phải có ít nhất 1 hộ (DB-15 ≥ 1) | `TestFN11_OutbreakVersions`, `TestCheckHouseholds`; vitest `outbreak.test.ts` | G1 |
| AC-04: chọn 3 hộ → DB-15 = 3, không sửa tay (giá trị gửi lên bị bỏ qua) | `TestFN11_MultiHousehold_252`; vitest `outbreak-ui.test.tsx` | G1 |
| DB-14: loại chăn nuôi lấy theo từng hộ (HCN-06), hộ nhập mới thì cán bộ chọn; chụp lúc ghi — hộ đổi loại sau đó không đổi báo cáo cũ (FN-10 BR-06) | `TestFN11_MultiHousehold_252` | G1 |
| DB-04 "nhập hộ chưa có" → hộ "Chờ bổ sung vị trí" trong danh sách, gắn ổ dịch tạo ra nó, dùng được ở báo cáo dịch bệnh khác (FN-10 BR-08, AC-09) | `TestFN11_MultiHousehold_252`; vitest `outbreak.test.ts` | G1 |
| FN-10 BR-07, AC-07: hộ vô hiệu hóa không chọn được ở DB-04 của báo cáo mới; vẫn hiện ở báo cáo cũ | `TestFN11_MultiHousehold_252` | G1 |
| AC-05, DB-16: bệnh nhóm thủy sản (loài của bệnh có nhóm `thuy_san`, FN-06) để trống DB-16 → không gửi được; ghi riêng nuôi trồng, khai thác; ≥ 0, ≤ 2 số lẻ; `1e100000` bị chặn trước khi parse | `TestFN11_MultiHousehold_252`, `TestCheckAqua`, `TestParseHectares`; vitest `outbreak.test.ts`, `outbreak-ui.test.tsx` | G1 |
| BR-02: mỗi lần cập nhật một phiên bản, mỗi phiên bản giữ tập hộ của nó; kết thúc/mở lại chụp lại tập hộ hiện hành | `TestFN11_MultiHousehold_252`, `TestFN11_OutbreakVersions` | G1 |
| BR-03 "tổng hợp vùng dịch lấy số của lần cập nhật mới nhất": DB-15, DB-16 lưu theo phiên bản, bản hiện hành là số mới nhất | `TestFN11_MultiHousehold_252` | G1; vùng dịch FN-18: CN-6 #254 |
| Ổ dịch cũ một hộ: giữ nguyên, API trả `legacy = true`, DB-15 hiện 1, DB-14/DB-16 trống; cập nhật diễn biến chọn hộ theo quy tắc mới | `TestFN11_LegacyOutbreak_252` | G1 |
| Không chuyển dữ liệu: số ổ dịch, số phiên bản, số phiên bản v1, tổng quy mô đàn, số con chết trước/sau migration bằng nhau; chuỗi băm sạch | `TestOutbreakMultiUpgradeKeepsLegacy` (pg-ephemeral) | G1 |
| Luồng phụ "xuất Word theo mẫu": Word BM-10 có số hộ (DB-15), bảng hộ (mã, chủ hộ che bớt, ấp, loại), DB-16 khi có | `TestWordDataHouseholds` | G1 |
| AC-01…AC-03, BR-01 (giữ nguyên từ W11.49): cảnh báo Khẩn, chặn thiếu vị trí, tiêu hủy > quy mô đàn | `TestFN11_OutbreakVersions`, `TestFN11_SuspectConfirmDismiss` | G1 |

Chưa nghiệm thu ở gói này: vùng dịch hiện số hộ, diện tích thủy sản bị ảnh hưởng và lớp trang trại trên bản đồ (FN-18 BR-02, CN-6
#254 — dùng `affected_households`, `aqua_*` của phiên bản mới nhất).

## FN-18 — Lớp trang trại, số hộ/diện tích bị ảnh hưởng của vùng dịch (CNTS-V4-FARM-LAYER #254)

Nguồn và giới hạn: [giả định](../kn/gia-dinh/CNTS-V4-FARM-LAYER.md), [ADR-0056](../adr/0056-ho-chan-nuoi-cnts-v4.md) mục 8–9.

| Tiêu chí BRD | Bằng chứng tự động | Phạm vi |
|---|---|---|
| Mô tả FN-18 "Lớp trang trại": hộ chăn nuôi Đang dùng loại trang trại (HCN-06) có vị trí check-in hiện hành (HCN-07); bật/tắt lớp, bấm xem chi tiết (B1, B2) | `TestCNTSMap_FarmLayerAndImpact_FN18`; vitest `map.test.ts` | G1 |
| FN-10 BR-08, BR-07: hộ Chờ bổ sung vị trí, hộ vô hiệu hóa, nông hộ không vào lớp trang trại; hộ đổi vị trí (H4) hiện ở vị trí check-in mới | `TestCNTSMap_FarmLayerAndImpact_FN18` | G1 |
| Mục I.8.1: đối tượng trang trại không mang họ tên, SĐT chủ hộ | `TestCNTSMap_FarmLayerAndImpact_FN18` | G1 |
| BR-02: vùng dịch, vùng bị uy hiếp, vùng đệm hiện số hộ bị ảnh hưởng (DB-15) và diện tích thủy sản bị ảnh hưởng nuôi trồng, khai thác (DB-16) của ổ dịch trong vùng; vùng chăn nuôi không có | `TestCNTSMap_FarmLayerAndImpact_FN18`, `TestImpactOf`; vitest `map.test.ts` | G1 |
| FN-11 BR-03: lấy số của lần cập nhật diễn biến mới nhất, không cộng dồn; ổ dịch cũ một hộ tính 1 hộ | `TestCNTSMap_FarmLayerAndImpact_FN18`, `TestImpactOf` | G1 |
| AC-03: chọn ngày cũ thấy số ảnh hưởng theo diễn biến của ngày đó | `TestCNTSMap_FarmLayerAndImpact_FN18` | G1 |
| AC-02: Cấp Xã chỉ thấy trang trại và số ảnh hưởng từ ổ dịch xã mình; tenant khác không thấy | `TestCNTSMap_FarmLayerAndImpact_FN18` | G1 |
| Trang trại, số ảnh hưởng trong vùng (đối tượng trong vùng đệm 3 km/10 km, khoảng cách tới tâm) | `TestCNTSMap_FarmLayerAndImpact_FN18`; vitest `map.test.ts` | G1 |
