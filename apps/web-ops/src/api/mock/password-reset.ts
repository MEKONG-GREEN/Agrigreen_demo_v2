// BACKEND GIẢ LẬP — tự đặt lại mật khẩu qua email (#255, ADR-0056; BRD 2.0 FN-CH-00.01 "Quên mật khẩu?"). Route CÔNG
// KHAI /v1/public/password-reset(/confirm): không cần token, chạy trước bước kiểm persona của mockFetch. Giả lập không gửi
// email: yêu cầu luôn 202; xác nhận nhận mọi mã đúng dạng (86 ký tự base64url) đúng một lần, mật khẩu dưới 12 ký tự → 422.
import { json, problem } from "./http";

const PATH = "/v1/public/password-reset";
const TOKEN = /^[A-Za-z0-9_-]{86}$/u;
const used = new Set<string>();

async function bodyOf(request: Request): Promise<Record<string, unknown> | null> {
  try {
    const v: unknown = JSON.parse(await request.text());
    return v && typeof v === "object" ? (v as Record<string, unknown>) : null;
  } catch {
    return null;
  }
}

/** Phản hồi giả lập cho route đặt lại mật khẩu; null khi không phải route này. */
export async function passwordResetMock(request: Request, url: URL): Promise<Response | null> {
  if (request.method !== "POST" || (url.pathname !== PATH && url.pathname !== `${PATH}/confirm`)) return null;
  const body = await bodyOf(request);
  if (!body) return problem(400, "bad_request", "Invalid JSON");
  if (url.pathname === PATH) {
    const email = typeof body.email === "string" ? body.email.trim() : "";
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/u.test(email) || email.length > 254) {
      return problem(422, "validation_failed", "Dữ liệu không hợp lệ", { errors: [{ field: "email", reason: "email không hợp lệ" }] });
    }
    return json({ status: "accepted" }, 202, { "Cache-Control": "no-store" });
  }
  const token = typeof body.token === "string" ? body.token : "";
  const password = typeof body.password === "string" ? body.password : "";
  if (!TOKEN.test(token) || used.has(token)) {
    return problem(410, "accounts.reset_link_invalid", "Liên kết đặt lại mật khẩu không hợp lệ, đã dùng hoặc đã hết hạn. Hãy yêu cầu liên kết mới.");
  }
  if ([...password].length < 12) {
    return problem(422, "accounts.password_policy", "Mật khẩu chưa đạt yêu cầu: tối thiểu 12 ký tự, khác tên đăng nhập và không trùng 5 mật khẩu gần nhất.");
  }
  used.add(token);
  return new Response(null, { status: 204, headers: { "Cache-Control": "no-store" } });
}
