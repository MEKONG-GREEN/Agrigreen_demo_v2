// Q4 removes TT ad hoc reporting. An exact route prevents the KN placeholder
// catch-all or streamed notFound() from returning a successful HTTP response.
export function GET() {
  return new Response("Không tìm thấy trang.", {
    status: 404,
    headers: {
      "Content-Type": "text/plain; charset=utf-8",
      "Cache-Control": "no-store",
    },
  });
}
