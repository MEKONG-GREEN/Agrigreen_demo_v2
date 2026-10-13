package port

// PubSchema là schema view công bố của module cho cap/analytics (pf/metrics) — R-DB-10, ADR-0031 §2, W6.3
// (pf 000124). Mỗi view là HỢP ĐỒNG có phiên bản như port: đổi bảng nội bộ không được làm vỡ view đã công bố;
// đổi cột/ý nghĩa = view v<N+1>_<tên> mới. Chỉ vai r_extension_pub (thành viên: r_metrics) đọc được.
//
// W11.10 (pf 000144): nguồn module cho KHTC — số liệu đã chốt (lần tổng hợp các xã lúc chốt) theo phòng × tỉnh ×
// khóa kỳ, kèm cờ chốt của kỳ nguồn. Hợp đồng cột: contracts/pubview/pf_extension_pub.consolidation_closed.v1.json.
//
// PFPUB (pf 20261013065758, ADR-0052): v1_closed_figures — cùng số liệu chiếu về họ hợp đồng thống nhất "closed_figures/v1"
// (cột, thứ tự giống hệt pf_forms_pub.v1_closed_figures của kỳ native). Bên đọc KHTC: ClosedFigures (closedfig.go).
//
//ag:pubview pf_extension_pub.field_report v1
//ag:pubview pf_extension_pub.task_event v1
//ag:pubview pf_extension_pub.task v1
//ag:pubview pf_extension_pub.directive_target_ack v1
//ag:pubview pf_extension_pub.consolidation_closed v1
//ag:pubview pf_extension_pub.closed_figures v1
const PubSchema = "pf_extension_pub"
