## BF-SWEEP-PRICE-SCREENS-FINAL — Lát Xã báo giá (#210)

Triển khai theo Function chi tiết BRD Báo giá v2.0, FN-AD-04.02: B3, luồng ngoại lệ ngày hiệu lực,
BR-02 (giữ lịch sử), BR-03 (màn chỉ hiển thị cán bộ). Phần ngoài lát này tiếp tục tại
[#257](https://github.com/MEKONG-GREEN/Agrigreen_demo_v2/issues/257); không nghiệm thu toàn Function hoặc toàn module bằng lát giao diện này.

### Tệp theo trình tự triển khai

| Bước | Tệp/phạm vi | FN/hợp đồng |
|---|---|---|
| 1 — Cấu trúc/dependency | Dùng kiến trúc hiện hữu; không thêm package | Không thay dependency/runtime |
| 2 — Data | Không đổi schema, migration, model hoặc payload API | FN-AD-04.02 BR-02; giữ versions/officers và quyền API hiện hành |
| 3 — Core | `apps/web-ops/src/kn/price/points/point-action.ts` | Tách guard single-flight/alive hiện hữu để form và dialog dùng chung idempotency; không đóng hộp mới bởi phản hồi cũ |
| 4 — Routing | `apps/web-ops/src/kn/price/points/point-routes.ts`; `apps/web-ops/src/app/kn/bao-gia/xa-bao-gia/[code]/phan-cong/route.ts` | BR-03: không nhận route chỉnh phân công; GET cũ kết thúc HTTP404, tránh fallback page streaming200 |
| 5 — UI | `PointDetail.tsx`, `PointsScreen.tsx`, `point-retire-dialog.tsx` trong thư mục points | B3: Ngưng ở detail và list dùng cùng dialog/API; cùng quyền, lý do, ngày hiệu lực, lỗi server, bộ lọc, lịch sử |
| 5 — Kiểm chứng | `point-lifecycle210.test.tsx`, `point-detail.test.tsx`, `point-routes.test.ts`; `apps/web-ops/tests/e2e/price-points182.spec.ts` | BR-02/03, không success toast sau Lưu, retry cùng khóa, ngày quá khứ, stale response, Tỉnh/retired chỉ đọc |

### Hành vi và giới hạn

- Detail chỉ cho Admin có `can_manage` hiện hành Ngưng bản ghi chưa ngưng. Gửi lý do/ngày qua API sẵn có;
  máy chủ vẫn kiểm quyền và ngày hiệu lực. Khi thành công tải lại dữ liệu theo đúng ngày/bộ lọc đang xem;
  bản ghi quá khứ và lịch sử vẫn đọc được. Không xóa dữ liệu.
- Gỡ link/form chỉnh phân công khỏi list/detail và parser route. Cán bộ, lịch sử phân công và Log vẫn chỉ đọc;
  API phục vụ phân quyền ở ngoài màn danh mục giữ nguyên. Không sửa màn tài khoản dùng chung.
- Lưu thêm/sửa vốn đã không yêu cầu success message; regression khóa hành vi này cùng kết quả lưu và điều hướng.
- Không có ô nhập số mới: ngày/lý do dùng validation hiện hữu; số lượng và phiên bản chỉ hiển thị. Guard decimal,
  giới hạn tài nguyên, payload, vai trò, tenant/phạm vi, clock/timer, workflow/scanner/baseline đều không thay.
- Nhập Excel xã, các màn mặt hàng/phiếu/báo cáo/điều tra và lịch sử nhóm vùng còn lại thuộc #257 hoặc gói đã quy hoạch riêng.

### Kiểm và chạy

Trong `apps/web-ops`:

```powershell
npm ci
npx vitest run src/kn/price/points
npm run typecheck
npm run lint
npm run check:api
npm test
npm run build:mock
npm run dev:mock
```

Từ gốc worktree, PostgreSQL tạm riêng cho native Price và cổng migration:

```powershell
$env:GOTOOLCHAIN = (Select-String go.mod '^toolchain\s+(\S+)').Matches[0].Groups[1].Value
.\scripts\pg-ephemeral.ps1 run go test -count=1 -p 1 -timeout 55m -v ./internal/kn/price/... ./migrations
```

Kiểm âm phải làm test FAIL khi cố ý bỏ guard quyền/liveness; phục hồi nguyên byte và chạy đối chứng.
Full web/E2E, cổng Go/lint-new0, PG thật0SKIP và CI đúng SHA là điều kiện trước tự review và ticket merge.
