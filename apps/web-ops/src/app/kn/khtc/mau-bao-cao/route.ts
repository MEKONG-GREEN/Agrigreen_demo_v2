// Provincial template entry retired by #220; Admin templates retain their own route.
export function GET() {
  return new Response("Không tìm thấy trang", { status: 404 });
}
