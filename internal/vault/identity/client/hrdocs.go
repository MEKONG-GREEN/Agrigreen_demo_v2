package client

// W11.24: kho tài liệu nhân sự HCTH (pf/hrm, FN-14). Tệp tài liệu đi qua pf/evidence (mã hóa phong bì, DEK seal bằng
// khóa công khai của chủ thể "kho tài liệu" của tenant); mở DEK (Unwrap) với mục đích PurposeHRRecords. Cơ sở xử lý là
// nghĩa vụ quản lý hồ sơ viên chức của cơ quan — không phải đồng ý của chủ thể (pf/evidence không kiểm đồng ý cho mục
// đích này).
const PurposeHRRecords Purpose = "hr_records"

// AreaHRDocs là mã địa bàn của chủ thể giữ khóa kho tài liệu nhân sự — không phân phối khóa xuống thiết bị theo xã.
const AreaHRDocs = "hrm_docs"
