// Các trang HRM thật được Next định tuyến trước. Trả status trực tiếp để
// URL không có màn không thành trang giữ chỗ200 hoặc404 sau khi đã stream.
export function GET() {
  return new Response("Không tìm thấy trang.", {
    status: 404,
    headers: {
      "Content-Type": "text/plain; charset=utf-8",
      "Cache-Control": "no-store",
    },
  });
}
