export function GET() {
  return new Response("Không tìm thấy", {
    status: 404,
    headers: { "Content-Type": "text/plain; charset=utf-8" },
  });
}
