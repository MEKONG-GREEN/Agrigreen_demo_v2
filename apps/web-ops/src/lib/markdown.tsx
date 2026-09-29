// Hiển thị Markdown của bài viết ở chế độ TẮT HTML thô (như App HTX — ContentVersion.body đã lọc allow-list
// ở máy chủ). Chỉ hỗ trợ tập con: tiêu đề #/##/###, đoạn, danh sách -/*/1., **đậm**, *nghiêng*, [liên kết](url)
// với http/https/mailto. Mọi thẻ HTML hiện nguyên văn dưới dạng chữ (React tự thoát ký tự).
import type { ReactNode } from "react";

export type MdBlock =
  | { kind: "h"; level: 1 | 2 | 3; text: string }
  | { kind: "p"; text: string }
  | { kind: "ul" | "ol"; items: string[] };

/** Tách khối theo dòng trống / tiền tố dòng. */
export function parseBlocks(src: string): MdBlock[] {
  const out: MdBlock[] = [];
  let para: string[] = [];
  let list: { kind: "ul" | "ol"; items: string[] } | null = null;
  const flush = () => {
    if (para.length) out.push({ kind: "p", text: para.join(" ") });
    para = [];
    if (list) out.push(list);
    list = null;
  };
  for (const raw of src.replace(/\r\n?/g, "\n").split("\n")) {
    const line = raw.trim();
    if (!line) {
      flush();
      continue;
    }
    const h = /^(#{1,3})\s+(.*)$/.exec(line);
    if (h) {
      flush();
      out.push({ kind: "h", level: h[1]!.length as 1 | 2 | 3, text: h[2]!.trim() });
      continue;
    }
    const ul = /^[-*]\s+(.*)$/.exec(line);
    const ol = /^\d+[.)]\s+(.*)$/.exec(line);
    if (ul || ol) {
      const kind = ul ? "ul" : "ol";
      if (para.length) {
        out.push({ kind: "p", text: para.join(" ") });
        para = [];
      }
      if (!list || list.kind !== kind) {
        if (list) out.push(list);
        list = { kind, items: [] };
      }
      list.items.push((ul ?? ol)![1]!.trim());
      continue;
    }
    if (list) {
      out.push(list);
      list = null;
    }
    para.push(line);
  }
  flush();
  return out;
}

/** Liên kết an toàn: http, https, mailto. Còn lại (javascript:, data:, att:…) → không phải liên kết. */
export function safeHref(url: string): string | null {
  const u = url.trim();
  return /^(https?:\/\/|mailto:)/i.test(u) ? u : null;
}

const INLINE = /(\*\*([^*]+)\*\*|\*([^*]+)\*|\[([^\]]+)\]\(([^)\s]+)\))/g;

/** Định dạng trong dòng thành phần tử React (không bao giờ dùng HTML thô). */
export function renderInline(text: string, keyPrefix = "i"): ReactNode[] {
  const out: ReactNode[] = [];
  let last = 0;
  let n = 0;
  for (const m of text.matchAll(INLINE)) {
    const at = m.index ?? 0;
    if (at > last) out.push(text.slice(last, at));
    const key = `${keyPrefix}-${n++}`;
    if (m[2] !== undefined) out.push(<strong key={key}>{m[2]}</strong>);
    else if (m[3] !== undefined) out.push(<em key={key}>{m[3]}</em>);
    else {
      const href = safeHref(m[5] ?? "");
      out.push(
        href ? (
          <a key={key} href={href} target="_blank" rel="noopener noreferrer">
            {m[4]}
          </a>
        ) : (
          <span key={key}>{m[4]}</span>
        ),
      );
    }
    last = at + m[0].length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

export function Markdown({ source }: { source: string }) {
  return (
    <>
      {parseBlocks(source).map((b, i) => {
        const k = `b${i}`;
        if (b.kind === "h") {
          const H = b.level === 1 ? "h2" : b.level === 2 ? "h3" : "h4";
          return <H key={k}>{renderInline(b.text, k)}</H>;
        }
        if (b.kind === "p") return <p key={k}>{renderInline(b.text, k)}</p>;
        const L = b.kind;
        return (
          <L key={k}>
            {b.items.map((it, j) => (
              <li key={`${k}-${j}`}>{renderInline(it, `${k}-${j}`)}</li>
            ))}
          </L>
        );
      })}
    </>
  );
}
