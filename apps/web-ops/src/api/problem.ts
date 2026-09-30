// Lỗi RFC 9457 (application/problem+json) — bảng mã ở docs/errors.md.
// Mã lỗi là hợp đồng: hiển thị thông báo tiếng Việt theo `code`, luôn kèm trace_id (R-63).

export interface ProblemFieldError {
  field?: string;
  reason?: string;
}

export interface ProblemDetails {
  type: string;
  title: string;
  status: number;
  code: string;
  detail?: string;
  instance?: string;
  trace_id?: string;
  errors?: ProblemFieldError[];
  /** 429: số giây phải chờ (thân `retry_after_s` hoặc header Retry-After). */
  retry_after_s?: number;
}

/** Thông báo tiếng Việt theo mã lỗi (docs/errors.md). */
export const PROBLEM_MESSAGES: Readonly<Record<string, string>> = {
  bad_request: "Yêu cầu không hợp lệ. Vui lòng kiểm tra lại dữ liệu gửi đi.",
  idempotency_key_required: "Thiếu khóa chống gửi trùng (Idempotency-Key). Vui lòng tải lại trang và thử lại.",
  unauthenticated: "Phiên đăng nhập đã hết hạn hoặc không hợp lệ. Vui lòng đăng nhập lại.",
  forbidden: "Bạn không có quyền thực hiện thao tác này.",
  mfa_required:
    "Chức năng quản trị (Admin API) bắt buộc xác thực hai yếu tố. Phiên hiện tại mới đăng nhập bằng mật khẩu — vui lòng đăng xuất rồi đăng nhập lại kèm mã OTP.",
  self_approval_forbidden: "Không được tự phê duyệt thay đổi do chính mình đề xuất. Cần một người phê duyệt khác.",
  not_found: "Không tìm thấy dữ liệu yêu cầu.",
  method_not_allowed: "Thao tác không được hỗ trợ.",
  idempotency_in_progress: "Yêu cầu trước đó đang được xử lý. Vui lòng đợi giây lát rồi tải lại.",
  conflict: "Dữ liệu đã bị thay đổi bởi người khác. Vui lòng tải lại và thử lại.",
  payload_too_large: "Dữ liệu gửi lên vượt quá dung lượng cho phép.",
  validation_failed: "Dữ liệu chưa hợp lệ. Vui lòng kiểm tra các trường được đánh dấu.",
  idempotency_key_reused: "Khóa chống gửi trùng đã được dùng cho một yêu cầu khác. Vui lòng thử lại.",
  internal_error: "Hệ thống gặp lỗi. Vui lòng thử lại sau hoặc gửi mã tra cứu cho bộ phận hỗ trợ.",
  "party.consent_not_found": "Không tìm thấy bản ghi đồng ý.",
  "party.consent_id_reused": "Mã bản ghi đồng ý đã được dùng cho bản ghi khác.",
  // pf/masterdata
  "masterdata.admin_unit_not_found": "Không tìm thấy đơn vị hành chính có hiệu lực với mã này.",
  "masterdata.catalog_not_found": "Danh mục không tồn tại.",
  "masterdata.param_version_not_found": "Không tìm thấy phiên bản tham số.",
  "masterdata.param_invalid_transition": "Trạng thái phiên bản không cho phép thao tác này (có thể đã được người khác xử lý). Vui lòng tải lại.",
  "masterdata.param_not_setter": "Chỉ người đặt tham số mới được gửi duyệt phiên bản này.",
  "masterdata.param_percent_thresholds": "Ngưỡng % phải tăng dần và nằm trong khoảng 0–100%.",
  "masterdata.param_no_default": "Tham số này không có giá trị mặc định để khôi phục.",
  "masterdata.param_correction_required":
    "Đã có phiên bản được duyệt cùng ngày hiệu lực. Nhập “Lý do sửa” (ít nhất 10 ký tự) để ghi bản ghi bù.",
  // pf/party
  "party.org_not_found": "Không tìm thấy tổ chức/HTX, hoặc nằm ngoài phạm vi được phân quyền.",
  "party.org_state_conflict": "Trạng thái tổ chức không cho phép thao tác này (hoặc không có gì thay đổi).",
  "party.out_of_scope": "Thao tác nằm ngoài phạm vi TW → Tỉnh → Xã được phân quyền.",
  "party.import_invalid":
    "Tệp nhập sai mẫu (thiếu cột bắt buộc, không phải CSV UTF-8/.xlsx, tệp .xls cũ, có ô công thức hoặc quá số dòng). Cả tệp bị từ chối.",
  "party.dependency_unavailable": "Dịch vụ danh bạ tổ chức/kho định danh tạm thời chưa sẵn sàng. Vui lòng thử lại sau.",
  "party.cooperative_name_taken": "Tên HTX này đã tồn tại, vui lòng chọn tên khác.",
  "party.household_phone_taken": "SĐT đã được sử dụng bởi nông hộ khác",
  "party.household_contact_unavailable": "Nông hộ chưa có số điện thoại liên hệ trong kho định danh — không gọi nhanh được.",
  "party.household_not_found": "Không tìm thấy nông hộ, hoặc nông hộ nằm ngoài phạm vi được phân quyền.",
  "reports.recipient_not_in_cooperative": "Email đăng ký nhận báo cáo không hợp lệ hoặc gửi thất bại, vui lòng kiểm tra lại.",
  // pf/geo
  "geo.admin_boundary_not_found": "Chưa nhập ranh giới hành chính cho đơn vị này.",
  "geo.plot_not_found": "Không tìm thấy thửa, hoặc thửa nằm ngoài phạm vi.",
  "geo.out_of_scope": "Thửa/HTX nằm ngoài phạm vi xã được phân quyền.",
  "geo.plot_state_conflict": "Thửa đã ngừng dùng hoặc đã ở đúng trạng thái yêu cầu.",
  "geo.overlap_cross_coop":
    "Ranh giới chồng lấn thửa của HTX khác vượt ngưỡng cho phép — không thể lưu. Cần hiệu chỉnh ranh giới hoặc xử lý với HTX liên quan.",
  "geo.overlap_needs_confirm":
    "Ranh giới chồng lấn thửa cùng HTX hoặc thửa nháp chưa gán. Bạn có thể xác nhận vẫn lưu — hệ thống sẽ mở xung đột để xem xét.",
  "geo.area_deviation_needs_confirm":
    "Diện tích đo lệch quá 5% so với diện tích khai báo. Kiểm tra lại, hoặc xác nhận vẫn lưu nếu số đo là đúng.",
  "geo.version_conflict": "Ranh giới đã được người khác cập nhật. Vui lòng tải lại để sửa trên phiên bản mới nhất.",
  "geo.dependency_unavailable": "Dịch vụ bản đồ tạm thời chưa sẵn sàng. Vui lòng thử lại sau.",
  // pf/extension, pf/content, pf/mechanization
  "extension.out_of_scope": "Dữ liệu nằm ngoài phạm vi TW → Tỉnh → Xã được phân quyền.",
  "extension.dependency_unavailable": "Dịch vụ khuyến nông tạm thời chưa sẵn sàng. Vui lòng thử lại sau.",
  "content.document_not_found": "Tài liệu không tồn tại, đã gỡ hoặc nằm ngoài phạm vi.",
  "content.dependency_unavailable": "Thư viện số tạm thời chưa sẵn sàng. Vui lòng thử lại sau.",
  // pf/metrics (G3)
  "metrics.unknown_metric": "Không có định nghĩa chỉ số này (hoặc chưa có phiên bản hiệu lực vào ngày đã chọn).",
  "metrics.out_of_scope": "Đơn vị đã chọn nằm ngoài phạm vi được phân quyền (toàn quốc chỉ dành cho cán bộ cấp TW).",
  "metrics.range_too_large": "Khoảng ngày không hợp lệ: tối đa 400 ngày và ngày bắt đầu không sau ngày kết thúc.",
  "metrics.unknown_layer": "Lớp bản đồ không tồn tại.",
  "metrics.future_date": "Không thể xem lại một ngày trong tương lai.",
  "metrics.not_closed": "Ngày nghiệp vụ chưa kết thúc — chưa có giá trị chốt.",
  "metrics.dependency_unavailable": "Dịch vụ chỉ số/lịch sử bản đồ tạm thời chưa sẵn sàng. Vui lòng thử lại sau.",
  // pf/mrv (G3)
  "mrv.out_of_scope": "Thửa/vụ nằm ngoài phạm vi TW → Tỉnh → Xã được phân quyền.",
  "mrv.season_not_found": "Không tìm thấy vụ (chưa được liên kết vào MRV) hoặc vụ nằm ngoài phạm vi.",
  "mrv.farm_log_not_found": "Nhật ký HTX này chưa được liên kết vào vụ.",
  "mrv.evidence_not_found": "Không tìm thấy minh chứng của cán bộ, hoặc minh chứng nằm ngoài phạm vi.",
  "mrv.evidence_superseded": "Minh chứng đã có bản ghi bù mới hơn — hãy đối chiếu với bản hiện hành.",
  "mrv.comparison_exists": "Cặp nhật ký ↔ minh chứng đã có kết quả đối chiếu. Muốn đổi kết quả, ghi bản ghi bù kèm lý do sửa.",
  "mrv.comparison_superseded": "Kết quả đối chiếu này đã được sửa bằng bản ghi bù mới hơn. Vui lòng tải lại.",
  "mrv.comparison_not_found": "Không tìm thấy kết quả đối chiếu cần sửa.",
  "mrv.method_not_found": "Không tìm thấy phiên bản phương pháp ước tính.",
  "mrv.method_invalid_transition": "Phiên bản phương pháp đã được duyệt/từ chối trước đó (bất biến). Vui lòng tải lại.",
  "mrv.dependency_unavailable": "Dịch vụ MRV tạm thời chưa sẵn sàng. Vui lòng thử lại sau.",
  "mechanization.run_not_found": "Chưa có kết quả cân đối đã lưu cho vụ này.",
  "mechanization.dependency_unavailable": "Dịch vụ Bản đồ CGH tạm thời chưa sẵn sàng. Vui lòng thử lại sau.",
  "mechanization.forecast_not_found": "Chưa có lần dự báo đã lưu này. Quản trị CGH chạy dự báo trên Admin API.",
  "extension.directive_not_found": "Không tìm thấy chỉ đạo, hoặc bạn không thuộc danh sách nhận/giám sát.",
  "extension.not_directive_target": "Bạn không thuộc danh sách nhận chỉ đạo này nên không xác nhận được.",
  "extension.no_directive_targets": "Vùng đã chọn chưa có cán bộ nào trong danh bạ — không ban hành chỉ đạo rỗng.",
  "extension.task_not_found": "Không tìm thấy nhiệm vụ, hoặc nhiệm vụ nằm ngoài phạm vi.",
  "content.library_item_not_found": "Tài liệu chưa có trong danh mục cá nhân.",
  // pf/notify
  "notify.notification_not_found": "Thông báo không còn (đã quá 90 ngày hoặc ngoài phạm vi).",
  "notify.out_of_scope": "Đơn vị đã chọn nằm ngoài phạm vi phụ trách.",
  "notify.nudge_target_not_found": "Không có thiết bị đang nhận thông báo đẩy với mã này (hoặc ngoài phạm vi).",
  "notify.nudge_rate_limited": "Đích này vừa được yêu cầu đồng bộ (tối đa 1 lần/5 phút, 30 lần/giờ mỗi người). Vui lòng đợi rồi thử lại.",
  "notify.dependency_unavailable": "Dịch vụ thông báo chưa nối sổ thiết bị. Vui lòng thử lại sau.",
  // pf/sync (Admin)
  "sync.conflict_not_found": "Không tìm thấy xung đột đồng bộ.",
  "sync.conflict_state": "Xung đột đã được đóng trước đó. Vui lòng tải lại.",
  "sync.conflict_resolution_invalid":
    "Cách xử lý không hợp lệ: “Giữ phiên bản” phải chọn một bản ghi của xung đột; “Bản ghi bù” không được trỏ lại bản ghi của xung đột.",
  "geo.conflict_not_found": "Không tìm thấy xung đột ranh giới trong phạm vi.",
  "geo.conflict_state": "Xung đột ranh giới đã được xử lý.",
  "geo.format_invalid": "Tệp ranh giới không đọc được (Shapefile phải là gói .zip gồm .shp, .dbf, .prj; tối đa 200 đối tượng).",
  "geo.format_unsupported": "Không nhận ra định dạng tệp — hỗ trợ GeoJSON, KML, Shapefile (.zip).",
  "geo.crs_unsupported": "Hệ toạ độ không phải WGS84 (EPSG:4326), ví dụ VN-2000. Chuyển sang EPSG:4326 (QGIS/ArcGIS) trước khi tải lên.",
  // pf/accounts
  "accounts.out_of_scope": "Tổ chức hoặc tài khoản nằm ngoài phạm vi TW → Tỉnh → Xã bạn phụ trách.",
  "accounts.role_not_grantable": "Vai trò vượt quyền cấp của bạn (hoặc tài khoản đích đang có vai bạn không được cấp).",
  "accounts.self_action": "Không được tự khóa, mở khóa hoặc đổi đơn vị/vai của chính mình.",
  "accounts.user_not_found": "Không tìm thấy tài khoản.",
  "accounts.user_exists": "Tên đăng nhập hoặc email đã có.",
  "accounts.phone_taken": "Số điện thoại này đã được đăng ký cho một tài khoản khác.",
  "accounts.state_conflict": "Trạng thái tài khoản không cho phép thao tác này (đã khóa, đang hoạt động, hoặc đang khóa khi gửi liên kết).",
  "accounts.org_inactive": "Tổ chức đang bị vô hiệu hóa — không gán hoặc mở khóa được tài khoản.",
  "accounts.org_invalid": "Không có tổ chức này trong tenant.",
  "accounts.import_invalid":
    "Tệp nhập sai mẫu (thiếu cột email, org_ref, roles, full_name; không phải CSV/XLSX; không có dòng hoặc quá 200 dòng). Cả tệp bị từ chối.",
  "accounts.email_failed": "Máy chủ không gửi được email liên kết kích hoạt. Vui lòng thử lại sau.",
  "accounts.dependency_unavailable": "Dịch vụ tài khoản (Keycloak) tạm thời chưa sẵn sàng. Vui lòng thử lại sau.",
  // pf/reports
  "reports.template_not_found": "Không có mẫu báo cáo này.",
  "reports.template_retired": "Mẫu đã ngừng dùng — không xuất và không lập lịch được.",
  "reports.format_unsupported": "Mẫu không hỗ trợ định dạng đã chọn.",
  "reports.export_not_found": "Không tìm thấy việc xuất.",
  "reports.export_not_ready": "Tệp chưa dựng xong hoặc việc xuất bị lỗi — chưa tải được.",
  "reports.export_quota_exceeded":
    "Bạn đã dùng hết hạn mức xuất báo cáo trong ngày (mặc định 50 lần, tính theo giờ Việt Nam). Vui lòng thử lại vào ngày mai hoặc liên hệ quản trị.",
  "reports.out_of_scope": "Đơn vị đã chọn nằm ngoài phạm vi phụ trách, hoặc tài khoản chưa gắn đơn vị.",
  "reports.schedule_not_found": "Không tìm thấy lịch gửi.",
  "reports.unit_invalid": "Mã đơn vị hành chính không còn hiệu lực hoặc không phải tỉnh/xã.",
  "reports.dependency_unavailable": "Dịch vụ báo cáo tạm thời chưa sẵn sàng. Vui lòng thử lại sau.",
  // pf/geo G2 (ranh giới HTX, kho/Hub, sức chứa, đường bộ)
  "geo.cooperative_boundary_not_found": "HTX chưa có ranh giới, hoặc ranh giới nằm ngoài phạm vi được phân quyền.",
  "geo.cooperative_boundary_exists": "HTX đã có ranh giới — dùng “Sửa ranh giới” để lưu phiên bản mới kèm lý do.",
  "geo.facility_not_found": "Không tìm thấy kho/Hub.",
  "geo.facility_code_exists": "Mã kho/Hub đã được dùng (mã giữ mãi, kể cả khi đã ngừng dùng).",
  "geo.hub_not_found": "Hub MKG chưa được nhận qua van công bố nên chưa nhập tay sức chứa được.",
  "geo.road_not_found": "Không tìm thấy tuyến đường bộ.",
  "geo.feature_state_conflict": "Trạng thái đối tượng không cho phép thao tác này (đã ngừng dùng hoặc chưa ngừng dùng). Vui lòng tải lại.",
  "geo.capacity_nothing_to_release": "Không có số sức chứa Admin nhập tay đang áp dụng để nhả cho số đồng bộ.",
  "geo.invalid_geometry": "Hình vẽ không hợp lệ (tự cắt, vòng sai, không khép kín hoặc quá 5.000 đỉnh).",
  "geo.too_few_points": "Hình vẽ chưa đủ điểm phân biệt hoặc các điểm thẳng hàng.",
  "geo.area_out_of_range": "Diện tích ngoài khoảng cho phép.",
  "geo.cooperative_invalid": "HTX không tồn tại, đã ngừng hoạt động hoặc ngoài phạm vi.",
  // pf/waterway
  "waterway.segment_not_found": "Không tìm thấy tuyến sông/kênh.",
  "waterway.structure_not_found": "Không tìm thấy công trình vượt sông.",
  "waterway.revision_conflict": "Tuyến đã được người khác sửa. Vui lòng tải lại rồi sửa trên bản mới nhất.",
  "waterway.segment_retired": "Tuyến đã ngừng dùng — không gắn công trình được.",
  "waterway.structure_off_segment": "Công trình nằm quá xa tuyến (quá 500 m).",
  "waterway.build_not_found": "Không tìm thấy lượt dựng mạng.",
  "waterway.network_version_not_found": "Không có phiên bản mạng đường thủy với số này (hoặc chưa phát hành phiên bản nào).",
  "waterway.version_not_publishable": "Chỉ phát hành được phiên bản ở trạng thái “Đã dựng” (phiên bản này đã phát hành hoặc đã bị thay).",
  "waterway.params_invalid": "Tham số dựng mạng không nhất quán (ngưỡng bắt điểm/nối tự động/báo cáo, lớp phương tiện). Kiểm tra tham số đường thủy.",
  "waterway.dependency_unavailable": "Dịch vụ mạng đường thủy tạm thời chưa sẵn sàng. Vui lòng thử lại sau.",
  // pf/simulation
  "simulation.hub_not_found": "Không tìm thấy Hub ứng viên.",
  "simulation.hub_retired": "Hub ứng viên đã ngừng dùng — không gán vào kịch bản được.",
  "simulation.scenario_not_found": "Không tìm thấy kịch bản.",
  "simulation.hub_not_attached": "Hub chưa gán (hoặc đã gỡ) khỏi kịch bản.",
  "simulation.scenario_without_hubs": "Kịch bản chưa có Hub nào — gán ít nhất một Hub trước khi chạy mô phỏng.",
  "simulation.baseline_scenario_invalid": "Kịch bản dùng làm baseline không tồn tại.",
  "simulation.simulation_not_found": "Không tìm thấy lượt mô phỏng.",
  "simulation.result_not_ready": "Lượt mô phỏng chưa có kết quả (đang chờ, đang chạy hoặc bị lỗi).",
  "simulation.params_unapproved":
    "Không thể đánh dấu Chính thức: ảnh chụp đầu vào còn tham số chưa có phiên bản được phê duyệt (CGH FN-01 BR-04). Kết quả vẫn dùng để tham khảo.",
  "simulation.dependency_unavailable": "Chưa nối nguồn cung HTX × vụ cho mô phỏng. Vui lòng thử lại sau.",
  "simulation.no_supply": "Không có HTX có toạ độ nằm trong bán kính phục vụ của các Hub.",
  // pf/cropcycle (lớp giai đoạn mùa vụ)
  "cropcycle.out_of_scope": "Nông hộ hoặc lô ruộng này không thuộc phạm vi quản lý của bạn.",
  // pf/cropcycle (App HTX)
  "cropcycle.sop_not_found": "Không tìm thấy quy trình SOP (hoặc chưa ban hành / là quy trình riêng của HTX khác).",
  "cropcycle.sop_name_taken": "Tên SOP này đã tồn tại, vui lòng chọn tên khác.",
  "cropcycle.sop_code_taken": "Mã SOP đã tồn tại.",
  "cropcycle.sop_step_order": "Thứ tự bước không hợp lệ, vui lòng kiểm tra lại.",
  "cropcycle.sop_harvest_step_missing": "Quy trình phải có bước thu hoạch để kế hoạch sản xuất có điểm kết thúc.",
  "cropcycle.sop_state": "Trạng thái quy trình không cho phép thao tác này (bản đã phát hành không sửa được — hãy tạo phiên bản mới).",
  "cropcycle.sop_not_published": "Quy trình chưa được ban hành — không áp dụng vào vụ được.",
  "cropcycle.no_sop_for_variety": "Chưa có quy trình SOP chuẩn đã ban hành cho giống lúa này — chọn quy trình khi mở vụ.",
  "cropcycle.binding_invalid": "Chỉ gắn giống với mẫu SOP hệ thống đã ban hành, chưa ngừng dùng.",
  "cropcycle.season_not_found": "Không tìm thấy vụ trong phạm vi được phân quyền.",
  "cropcycle.plot_not_found": "Không tìm thấy lô ruộng.",
  "cropcycle.plot_invalid": "Lô ruộng chưa gán HTX, đã ngừng dùng hoặc chưa có diện tích.",
  "cropcycle.read_only": "Vai của bạn chỉ được xem dữ liệu mùa vụ.",
  "cropcycle.sowing_date_out_of_window": "Ngày gieo sạ phải trong khoảng 15 ngày trước đến 30 ngày sau ngày hiện tại.",
  "cropcycle.season_already_open": "Lô ruộng đang canh tác vụ khác, không thể mở vụ mới.",
  "cropcycle.previous_season_not_completed": "Chỉ sao chép được cấu hình khi lô ruộng đã hoàn thành vụ trước.",
  "cropcycle.no_previous_season": "Lô ruộng chưa có vụ nào đã hoàn thành để sao chép cấu hình.",
  "cropcycle.cooperative_inactive": "HTX không tồn tại hoặc đã ngừng hoạt động.",
  "cropcycle.catalog_invalid": "Mã giống / vụ / máy không có hoặc đã ngừng dùng trong danh mục.",
  "cropcycle.season_not_open": "Lô ruộng này chưa được mở vụ. Vui lòng mở vụ trước khi ghi nhật ký.",
  "cropcycle.season_state": "Trạng thái vụ không cho phép thao tác này (vụ đã đóng hoặc đã khai sản lượng).",
  "cropcycle.concurrent_edit": "Bản ghi đã được sửa bởi người khác — hãy tải phiên bản hiện hành rồi sửa lại.",
  "cropcycle.locked_fields": "Tọa độ GPS và thời gian không thể chỉnh sửa sau khi lưu.",
  "cropcycle.log_not_found": "Không tìm thấy nhật ký trong phạm vi.",
  "cropcycle.log_state": "Nhật ký không còn ở trạng thái chờ duyệt (đã được xử lý hoặc đã có phiên bản mới). Vui lòng tải lại.",
  "cropcycle.self_review": "Người ghi nhật ký không được tự duyệt — cần một quản lý HTX khác duyệt.",
  "cropcycle.batch_empty": "Không có lô ruộng hợp lệ trong lượt ghi hàng loạt.",
  "cropcycle.harvest_too_early": "Lô ruộng chưa đến giai đoạn thu hoạch, chưa thể khai báo sản lượng.",
  "cropcycle.phi_violation": "Chưa hết thời gian cách ly sau phun thuốc — chưa được thu hoạch.",
  "cropcycle.yield_negative": "Sản lượng không được âm.",
  "cropcycle.yield_anomaly": "Sản lượng bất thường — xác nhận lại trước khi lưu.",
  "cropcycle.harvest_not_found": "Không tìm thấy khai báo sản lượng.",
  "cropcycle.import_invalid": "Tệp nhập sản lượng không hợp lệ (không đọc được, có công thức, quá 200 dòng / 2 MB, thiếu cột mã lô/mã vụ hoặc sản lượng).",
  "cropcycle.id_reused": "Mã bản ghi đã dùng cho bản ghi khác.",
  "cropcycle.dependency_unavailable": "Dịch vụ mùa vụ tạm thời chưa sẵn sàng. Vui lòng thử lại sau.",
  // pf/accounts — tài khoản nông dân
  "accounts.household_invalid": "Nông hộ không có, đã ngừng, chưa là thành viên HTX đang hoạt động, hoặc SĐT không khớp nông hộ nào.",
  "accounts.phone_household_mismatch": "SĐT không phải SĐT chủ hộ của nông hộ đã chọn.",
  "accounts.farmer_exists": "Nông hộ đã có tài khoản nông dân đang dùng — dùng “Phát lại QR” nếu nông dân mất liên kết.",
  "accounts.farmer_not_found": "Không tìm thấy tài khoản nông dân, hoặc ngoài HTX/phạm vi của bạn.",
  "accounts.activation_link_unavailable": "Chưa phát được liên kết kích hoạt (máy chủ đăng nhập chưa sẵn sàng). Tài khoản đã tạo — phát lại QR sau.",
  // pf/masterdata — nhập danh mục
  "masterdata.catalog_import_invalid":
    "Tệp nhập danh mục sai mẫu (thiếu cột catalog/code/name, có ô công thức, quá 500 dòng hoặc 2 MB). Cả tệp bị từ chối.",
  "masterdata.catalog_import_not_found": "Không tìm thấy lần nhập danh mục này.",
  // pf/sync — tệp minh chứng
  "sync.purpose_required": "Thiếu mục đích xử lý dữ liệu khi mở tệp minh chứng.",
  "sync.purpose_not_allowed": "Vai của bạn không được mở tệp minh chứng với mục đích này.",
  "sync.consent_missing": "Nông hộ chưa đồng ý chia sẻ dữ liệu cho mục đích này — không mở được ảnh.",
  "sync.file_not_found": "Không tìm thấy tệp minh chứng.",
  too_many_requests: "Quá nhiều yêu cầu. Vui lòng đợi rồi thử lại.",
  network_error: "Không kết nối được máy chủ. Vui lòng kiểm tra mạng và thử lại.",
  invalid_response: "Máy chủ trả về dữ liệu không đúng định dạng.",
};

/** Mã 409 của pf/geo mà người dùng được phép "Xác nhận vẫn lưu" (gửi lại với confirm_overlap: true). */
export const CONFIRMABLE_CODES: ReadonlySet<string> = new Set(["geo.overlap_needs_confirm", "geo.area_deviation_needs_confirm"]);

export function isConfirmable(p: { code: string } | null | undefined): boolean {
  return !!p && CONFIRMABLE_CODES.has(p.code);
}

const STATUS_FALLBACK: Readonly<Record<number, string>> = {
  400: "bad_request",
  401: "unauthenticated",
  403: "forbidden",
  404: "not_found",
  405: "method_not_allowed",
  409: "conflict",
  413: "payload_too_large",
  422: "validation_failed",
  429: "too_many_requests",
};

export class ApiProblem extends Error {
  readonly status: number;
  readonly code: string;
  readonly traceId: string | undefined;
  readonly fieldErrors: ProblemFieldError[];
  readonly problem: ProblemDetails;
  /** Giây phải chờ trước khi thử lại (429), nếu máy chủ cho biết. */
  readonly retryAfterS: number | undefined;

  constructor(problem: ProblemDetails) {
    super(problemMessage(problem));
    this.name = "ApiProblem";
    this.status = problem.status;
    this.code = problem.code;
    this.traceId = problem.trace_id;
    this.fieldErrors = problem.errors ?? [];
    this.problem = problem;
    this.retryAfterS = problem.retry_after_s;
  }
}

/** Header Retry-After: số giây hoặc HTTP-date (RFC 9110 §10.2.3). Sai dạng → undefined. */
export function parseRetryAfter(value: string | null | undefined, now: number = Date.now()): number | undefined {
  if (!value) return undefined;
  const v = value.trim();
  if (/^\d+$/.test(v)) return Number(v);
  const t = Date.parse(v);
  if (!Number.isFinite(t)) return undefined;
  return Math.max(0, Math.ceil((t - now) / 1000));
}

/** Thông báo cho người dùng: ưu tiên bảng mã; mã lạ thì dùng detail/title của server. */
const PREFER_DETAIL: ReadonlySet<string> = new Set(["party.out_of_scope", "party.import_invalid"]);

export function problemMessage(p: Pick<ProblemDetails, "code" | "status" | "detail" | "title">): string {
  // Thông báo nghiệp vụ của 422 (vd "Vui lòng nhập đầy đủ Tên tuyến và Phân loại." — docs/errors.md) nằm ở `detail`.
  if (p.code === "validation_failed" && p.detail) return p.detail;
  // Thông điệp AC nằm ở detail: "Bạn không có quyền truy cập nông hộ này." (HTX US-HH-01 AC-3), "Sai định dạng file: …" (US-HTX-02).
  if (PREFER_DETAIL.has(p.code) && p.detail) return p.detail;
  const known = PROBLEM_MESSAGES[p.code];
  if (known) return known;
  if (p.detail) return p.detail;
  if (p.title) return p.title;
  const byStatus = STATUS_FALLBACK[p.status];
  if (byStatus) return PROBLEM_MESSAGES[byStatus] ?? "";
  return PROBLEM_MESSAGES.internal_error ?? "Lỗi";
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** Lấy trace-id từ header W3C traceparent: 00-<trace-id>-<span-id>-<flags>. */
function traceIdFromHeaders(headers: Headers | undefined): string | undefined {
  if (!headers) return undefined;
  const explicit = headers.get("X-Trace-Id");
  if (explicit) return explicit;
  const tp = headers.get("traceparent");
  const m = tp ? /^[\da-f]{2}-([\da-f]{32})-[\da-f]{16}-[\da-f]{2}$/i.exec(tp.trim()) : null;
  return m?.[1];
}

/**
 * Chuẩn hoá body lỗi (đã parse JSON hoặc chuỗi) + response thành ProblemDetails.
 * Không bao giờ ném: body lạ vẫn ra một problem có code theo mã HTTP.
 */
export function toProblem(body: unknown, response?: Pick<Response, "status" | "headers">): ProblemDetails {
  const status = response?.status ?? (isRecord(body) && typeof body.status === "number" ? body.status : 500);
  const headerTrace = traceIdFromHeaders(response?.headers);
  const retryAfter =
    isRecord(body) && typeof body.retry_after_s === "number" ? body.retry_after_s : parseRetryAfter(response?.headers?.get("Retry-After"));
  if (isRecord(body) && typeof body.code === "string") {
    const errors = Array.isArray(body.errors)
      ? body.errors.filter(isRecord).map((e) => ({
          field: typeof e.field === "string" ? e.field : undefined,
          reason: typeof e.reason === "string" ? e.reason : undefined,
        }))
      : undefined;
    return {
      type: typeof body.type === "string" ? body.type : `https://errors.agrigreen.vn/${body.code}`,
      title: typeof body.title === "string" ? body.title : "",
      status: typeof body.status === "number" ? body.status : status,
      code: body.code,
      detail: typeof body.detail === "string" ? body.detail : undefined,
      instance: typeof body.instance === "string" ? body.instance : undefined,
      trace_id: typeof body.trace_id === "string" ? body.trace_id : headerTrace,
      errors,
      retry_after_s: retryAfter,
    };
  }
  const code = STATUS_FALLBACK[status] ?? (status >= 500 ? "internal_error" : "bad_request");
  return {
    type: `https://errors.agrigreen.vn/${code}`,
    title: "",
    status,
    code,
    trace_id: headerTrace,
    retry_after_s: retryAfter,
  };
}

/** Đọc một Response lỗi (thường là application/problem+json) thành ApiProblem. */
export async function parseProblemResponse(response: Response): Promise<ApiProblem> {
  let body: unknown = undefined;
  try {
    const text = await response.text();
    body = text ? JSON.parse(text) : undefined;
  } catch {
    body = undefined;
  }
  return new ApiProblem(toProblem(body, response));
}

export function networkProblem(): ApiProblem {
  return new ApiProblem({
    type: "https://errors.agrigreen.vn/network_error",
    title: "",
    status: 0,
    code: "network_error",
  });
}

/** Ánh xạ errors[] theo trường → thông điệp để gắn vào form. */
export function fieldErrorMap(p: ApiProblem): Record<string, string> {
  const out: Record<string, string> = {};
  for (const e of p.fieldErrors) {
    if (e.field) out[e.field] = e.reason ?? "Không hợp lệ";
  }
  return out;
}
