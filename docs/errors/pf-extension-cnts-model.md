### pf/extension (`dom/extension` — CT/DA, điểm trình diễn, hộ mô hình, cấp quyền CNTS, W11.47)

Dùng lại mã đã có của module: `extension.read_only`, `extension.out_of_scope` (xã/tỉnh ngoài phạm vi, gán địa bàn ngoài
tỉnh — FN-14; chủ nhiệm giao điểm/hộ của dự án khác — FN-14 BR-03), `extension.support_reason_required`,
`extension.dependency_unavailable` (chưa nối đơn vị hành chính, danh mục CNTS, kho định danh, danh bạ Cấp Xã, danh sách CT/DA
KHTC khi gắn `program_ref`).

| HTTP | code | Khi nào |
|---|---|---|
| 404 | `extension.cnts_project_not_found` | Không có CT/DA hoặc ngoài phạm vi người gọi |
| 422 | `extension.cnts_project_invalid` | CT/DA sai: mã, tên, loại, ngày, kinh phí; `program_ref` không có/đã ngừng trong danh sách KHTC; chủ nhiệm tạo dự án đứng tên người khác hoặc tự đổi chủ nhiệm; văn bản có SĐT/CCCD |
| 409 | `extension.cnts_project_version_conflict` | `base_version` khác phiên bản hiện hành; đóng/mở lại dự án đã ở trạng thái đó |
| 409 | `extension.cnts_project_code_taken` | Mã dự án đã thuộc dự án khác trong tenant |
| 409 | `extension.cnts_project_closed` | Sửa dự án, thêm/sửa điểm của dự án đã kết thúc |
| 404 | `extension.cnts_site_not_found` | Không có điểm trình diễn hoặc ngoài phạm vi (Cấp Xã: xã khác) |
| 422 | `extension.cnts_site_invalid` | Điểm sai: xã, tên, loại mô hình không có trong danh mục, quy mô/đơn vị, mẫu nhật ký, tọa độ ngoài Việt Nam, cán bộ theo dõi không phải Cấp Xã CNTS phụ trách xã, HTX không có trong `pf_cooperative_pub.v1_cooperatives`, đổi xã khi còn hộ |
| 409 | `extension.cnts_site_version_conflict` | `base_version` khác phiên bản hiện hành của điểm |
| 409 | `extension.cnts_site_closed` | Sửa điểm đã đóng; thêm/sửa hộ của điểm hoặc dự án đã đóng |
| 404 | `extension.cnts_household_not_found` | Không có hộ mô hình hoặc ngoài phạm vi (Cấp Xã có giới hạn hộ W11.07) |
| 422 | `extension.cnts_household_invalid` | Hộ sai: thiếu họ tên chủ hộ/`person_ref`, gửi cả hai, chủ thể không có ở kho định danh, quy mô/đơn vị, người ghi nhật ký không phải Cấp Xã CNTS phụ trách xã, đổi chủ hộ khi sửa, ghi chú có SĐT/CCCD |
| 409 | `extension.cnts_household_version_conflict` | `base_version` khác phiên bản hiện hành; rút/đưa lại hộ đã ở trạng thái đó |
| 409 | `extension.cnts_household_exists` | Chủ thể (cùng SĐT ở kho định danh) đã là hộ đang tham gia của điểm |
| 409 | `extension.cnts_household_withdrawn` | Sửa hộ đã rút khỏi mô hình |
| 404 | `extension.cnts_grant_not_found` | Không có quyền hoặc ngoài tỉnh người gọi (Cấp Xã chỉ xem quyền của mình) |
| 422 | `extension.cnts_grant_invalid` | Cấp quyền sai: vai ngoài 4 vai CNTS, thiếu xã, phạm vi không hợp vai (KSGM/tăng cường theo xã/cơ sở; theo dõi mô hình theo điểm/hộ; nông hộ đúng một hộ), cơ sở/điểm/hộ không thuộc xã được giao, biểu ngoài BM-01…BM-19, ngày hết hiệu lực trước ngày hiệu lực, người được cấp không phải tài khoản Cấp Xã CNTS, tự cấp cho mình, đổi vai khi sửa, người nhận việc trùng người bị thu hồi |
| 409 | `extension.cnts_grant_version_conflict` | `base_version` khác phiên bản hiện hành của quyền |
| 409 | `extension.cnts_grant_exists` | Người được cấp (hoặc người nhận việc) đã có quyền còn/chờ hiệu lực cùng vai trong tỉnh |
| 409 | `extension.cnts_grant_revoked` | Sửa hoặc thu hồi quyền đã thu hồi |
