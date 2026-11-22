# Nghiệm thu consumer policy accounts — BRD 2.0 (#196)

Phạm vi: nối hợp đồng kỹ thuật owner #195 vào các consumer dùng chung. Đối chiếu Function chi tiết HCTH v4.0 FN-04, FN-05 BR-04/AC-01, FN-07 BR-04/BR-07/AC-02 và trường HS-37. Không phải chứng nhận đầy đủ màn hình hoặc toàn bộ các FN.

| Tiêu chí | Bằng chứng tự động |
|---|---|
| Tạo/list/seed đơn vị dùng đúng loại, nhãn, loại có phòng ban, loại tổ chức và dữ liệu seed owner; update/ngừng có log | `TestKNUnitDelegationOwnerParityOnPostgres`, `TestUnitDelegationDefinitionsDriveHTTPAndLedgerOnPostgres` |
| Manager đúng phiên/phòng cấp quyền; người nhận có lần gán cùng đơn vị/phòng phủ toàn hạn; quyền và thời hạn từ owner | Các test PG trên; fixture định nghĩa khác chứng minh cả `GranteeRoles`, `Powers`, `MaxDuration` được dùng |
| Thu hồi chỉ người giao/Admin, replay kiểm actor/nội dung; hết hạn/thu hồi/mất assignment/mất quyền không giữ quyền trên ngữ cảnh tái dùng | `TestKNUnitDelegationOwnerParityOnPostgres` |
| Chỉ xem và hỗ trợ không cấp ủy quyền; tenant khác không dùng được ngữ cảnh | `TestKNUnitDelegationOwnerParityOnPostgres`; `TestUnitDelegationOwnerDoesNotBypassAdminMFAOnPostgres` |
| FN-07 BR-04/BR-07/AC-02, HS-37: HCTH/Admin, lãnh đạo đơn vị, nhân sự cùng đơn vị nội bộ và cán bộ xã được phân công; còn lại scope self | `TestKNUnitDelegationOwnerParityOnPostgres` so quyết định owner độc lập với kết quả truy vấn accounts |
| Provider decline/error không dùng quyền compatibility; callback no-op không cấp quyền cho phiên chưa gắn owner; quyết định thiếu dữ liệu bị từ chối trước DB; thuộc tính bị thu hồi ánh xạ đúng lỗi | `TestUnitDelegationConfiguredVerdictsAreFinal`, `TestUnitDelegationNoOpOwnerCannotAuthorizeNativeOrPartialContext`, `TestUnitDelegationIncompleteDecisionsFailBeforeDatabase`, `TestUnitDelegationUnhandledNativeKeepsContextRequired` |
| Startup từ chối định nghĩa sai; cấu hình từng instance không ảnh hưởng bridge khác | `TestUnitDelegationInvalidConfigurationStopsStartup`, `TestUnitDelegationDefinitionsDriveHTTPAndLedgerOnPostgres` |
| Create/grant/change/import CSV, required-actions và native HTX/MKG giữ parity khi bật provider; không đổi thứ tự role catalog | `TestProductionRoleCatalogHTTPParityOnPostgres`, `TestUnitDelegationRolloutRetainsNativeHTXAndMKGOnPostgres` |
| API và mọi worker constructor thật có nối provider, compatibility payload dở dang không thành authority | `TestBuildAccountRoleOrderThroughHTTP`, `TestWorkerAccountsUsesOwnerRoleOrder` |

Kết quả chạy đầy đủ, 0 SKIP, mutation FAIL/control PASS và CI/race Linux trên đúng SHA được đăng issue #196. [Cách chạy và giới hạn](../dev/accounts-unit-delegation-rollout.md): Keycloak/issuer kiểm thử, không thay thế realm sống; không suy diễn G1/4b/cutover hoặc retire #57 từ catalog/adapter tests.
