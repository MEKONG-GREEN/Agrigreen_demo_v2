package port

// PubSchema là schema view công bố của cap/forms (R-DB-10, W6.3; ADR-0053). Mỗi view là HỢP ĐỒNG có phiên bản như
// port: đổi bảng nội bộ không được làm vỡ view đã công bố; đổi cột/ý nghĩa = view v<N+1>_<tên> mới. Chỉ vai
// r_forms_pub đọc được (thành viên: r_metrics, r_agg do bootstrap cấp; r_extension — bên đọc KHTC, ADR-0053 §4).
//
// PFPUB (pf 20261013150108): pf_forms_pub.v1_closed_figures — số liệu ĐÃ CHỐT theo phòng × tỉnh × khóa kỳ của kỳ native
// (ảnh chụp đóng băng lúc chốt). Cùng họ hợp đồng "closed_figures/v1" với pf_extension_pub.v1_closed_figures (nguồn
// vòng đời báo cáo cũ): cột và thứ tự giống hệt — contracts/pubview/pf_forms_pub.closed_figures.v1.json.
//
//ag:pubview pf_forms_pub.closed_figures v1
const PubSchema = "pf_forms_pub"
