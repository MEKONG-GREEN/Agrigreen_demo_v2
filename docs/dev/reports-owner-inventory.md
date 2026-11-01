# Kiểm kê chủ nguồn báo cáo — KNSPLIT-2-REPORTS-OWNER

## Phạm vi và nguồn gốc

Khôi phục #54 từ `origin/dev` tại `857934416860007cfb2e1b886f682dea27c3ec95` trên máy mới. Checkpoint cũ `0fb3fad9` chỉ được mô tả trong bình luận issue, không có nhánh remote hoặc object tại máy mới; tài liệu này là kiểm kê lại cây mã hiện hành, không khẳng định đã phục hồi nguyên văn checkpoint. Không thêm registry hoặc thay hành vi production để hoàn thành ticket.

## Hợp đồng đang dùng

| Năng lực | Shared contract / nơi dùng | Đăng ký do KN sở hữu |
|---|---|---|
| Dựng nguồn và nhãn/cột | `reports/port` SourceBuilderRegistration, CatalogRegistration; `reports/app/source_builder.go`, `catalog.go` | `extension.ReportBuilders`, `extension.ReportCatalog` |
| Chính sách vai, phiên, dispatch | `reports/port` Policy, SessionResolver; `reports/app/policy_registry.go`, `session.go` | `extension.ReportPolicy`, `extension.ReportSessionResolver` |
| Mẫu lưới có sẵn | `reports/port` FormSeedRegistration; engine lưới và renderer dùng chung | `extension.ReportFormSeeds` |
| Kỳ, kết quả tổng hợp, lịch sử gửi | `reports/port/consolidations.go`; dispatch qua cổng | `kn.Consolidations()` và `rep.DispatchHistory()` tại assembly |
| Lịch xuất và job | Native scheduler, DLP, outbox và immutable job snapshot; policy nguồn kiểm quyền hiện hành | Owner có thể đăng ký SourceJobPolicy, không suy quyền từ realm hoặc bộ lọc client |

`internal/platform/reports/module.go` nhận các đăng ký qua Deps. `cmd/api-platform/assembly/modules.go` và `cmd/worker/reports.go` ghép đăng ký KN tường minh cùng nguồn native mechanization/cropcycle. Nhánh formimport của `cmd/worker/main.go` cũng nhận form seeds/catalog/policy; không cần dựng registry thứ hai.

Thiếu builder/catalog/session owner phải từ chối; lỗi callback không kích hoạt fallback. Các kiểm hiện có gồm SourceBuilderRequiresRegistration, ReportCatalogRequiresRegistration, policy/session registry và owner job-policy HTTP trước idempotency cache. Nguồn có private args không được lập lịch tới khi có hợp đồng freeze và authority cho từng lượt; pending delivery bị skipped bền vững theo chính sách đã có. Đây là hành vi bảo mật có chủ ý, không phải thiếu registry cần nới.

## Fixture #81 và ranh giới

`tests/platformdata/kn_consumers_test.go:TestKNReportSessionWithPublishedOrganizations` dựng SelectedSession qua `kn.NewSessionContext`; vai không hợp lệ giữ ca từ chối rõ ràng. Ma trận đối chiếu native/published qua tenant, org, role và operation có positive assertion cho tỉnh 89; tránh trường hợp cả hai resolver cùng từ chối khiến parity giả. Không thêm đọc Principal.KN làm fallback.

Không đổi renderer/grid, 35 golden báo cáo, frozen snapshot, Params/private args, schema/migration hoặc web. Không chuyển tenant, MRV, quyền D2 hoặc công bố G1/KNSPLIT-4b hoàn tất. `docs/dev/reports-owner-job-policy.md` tiếp tục là hợp đồng chi tiết cho owner job policy; #83 là consumer audit riêng, không phải lý do thêm cơ chế xuất trùng.

## Nghiệm chứng

Kết quả lệnh trên đúng SHA cuối được ghi trong bình luận Sẵn sàng của #54; tài liệu này không dùng log cũ từ máy trước làm bằng chứng chạy hiện tại. Bắt buộc chạy private PostgreSQL cho reports, reportforms, platformdata (gồm fixture #81), API assembly và migration gates; giữ cả lỗi hoặc SKIP nếu xuất hiện, không ghi PASS trước khi runner kết thúc. CI Linux kiểm race trên đúng SHA gửi review.
