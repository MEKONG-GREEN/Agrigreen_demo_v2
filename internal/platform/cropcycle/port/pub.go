package port

// PubSchema là schema view công bố của module cho cap/analytics (pf/metrics) — R-DB-10, ADR-0031 §2, W6.3
// (pf 000124). Mỗi view là HỢP ĐỒNG có phiên bản như port: đổi bảng nội bộ không được làm vỡ view đã công bố;
// đổi cột/ý nghĩa = view v<N+1>_<tên> mới. Chỉ vai r_cropcycle_pub (thành viên: r_metrics) đọc được.
//
//ag:pubview pf_cropcycle_pub.season_event v1
//ag:pubview pf_cropcycle_pub.harvest_declaration v1
const PubSchema = "pf_cropcycle_pub"
