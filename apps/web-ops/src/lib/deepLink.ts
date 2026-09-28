// Ánh xạ `deep_link` của thông báo (pf/notify, đường dẫn trong app — không phải URL web) sang trang web vận hành.
// Đường dẫn lạ hoặc sai dạng → null: giao diện chỉ mở hộp thư, không điều hướng theo dữ liệu tuỳ ý.

const UUID = "[0-9a-fA-F-]{8,64}";

const RULES: readonly { re: RegExp; to: (id: string) => string }[] = [
  { re: new RegExp(`^/extension/directives/(${UUID})$`), to: (id) => `/chi-dao?id=${id}` },
  { re: new RegExp(`^/documents/(${UUID})$`), to: (id) => `/thu-vien?doc=${id}` },
  { re: new RegExp(`^/extension/tasks/(${UUID})$`), to: (id) => `/nhiem-vu?task=${id}` },
  { re: new RegExp(`^/sync/conflicts/(${UUID})$`), to: (id) => `/quan-tri/xung-dot?id=${id}` },
  { re: new RegExp(`^/orgs/(${UUID})$`), to: (id) => `/quan-tri/to-chuc?org=${id}` },
];

export function deepLinkHref(deepLink: string | null | undefined): string | null {
  if (!deepLink) return null;
  const path = deepLink.trim().split(/[?#]/)[0] ?? "";
  for (const r of RULES) {
    const m = r.re.exec(path);
    if (m?.[1]) return r.to(encodeURIComponent(m[1]));
  }
  return null;
}
