import Link from "next/link";

export default function NotFound() {
  return (
    <div className="card sec">
      <h1 style={{ fontSize: 20, marginBottom: 6 }}>Không tìm thấy trang</h1>
      <p className="mute">Đường dẫn không tồn tại hoặc đã được thay đổi.</p>
      <p style={{ marginTop: 12 }}>
        <Link className="btn btn-o btn-sm" href="/">
          Về trang chính
        </Link>
      </p>
    </div>
  );
}
