// Nhận diện sản phẩm — TẠM, chờ khách (Q-19: bộ nhận diện TTKNQG). BRD §4.0 mục 5: "Khuyến nông số"
// và ảnh nền trên wireframe là tạm. Đổi ở MỘT chỗ này khi khách gửi tên, logo, ảnh nền, dòng footer.
export const BRAND = {
  /** Tên sản phẩm (tạm). */
  productName: "Khuyến nông số",
  /** Đơn vị sở hữu ở footer sidebar (tạm). */
  orgShort: "TTKN Quốc gia",
  orgFull: "Trung tâm Khuyến nông Quốc gia",
  year: 2026,
  /** Nhãn khối thương hiệu ở trang đăng nhập (Hình 3a). */
  systemTag: "Hệ thống khuyến nông",
  loginTitle: "Module Phòng Trồng trọt",
  loginLead: "Nhập báo cáo trồng trọt theo đúng mẫu, theo dõi tiến độ nộp và tổng hợp số liệu toàn tỉnh trên một hệ thống.",
} as const;

export const footerText = () => `© ${BRAND.year} ${BRAND.orgShort}`;
