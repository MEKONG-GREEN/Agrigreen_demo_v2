package port

// SyncEntityLibrary là entity chiều xuống "danh mục cá nhân" (KN US-LIB-05): tài
// liệu người dùng đã lưu để đọc ngoại tuyến. Chỉ giao cho CHÍNH người lưu; bỏ lưu
// → tombstone. id của thay đổi = document_ref.
const SyncEntityLibrary = "library_item"
