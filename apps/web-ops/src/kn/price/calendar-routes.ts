// Routes strictes cho FN-AD-04.04–06, dùng chung giữa page phía server và liên kết phía client.
export const PRICE_CONFIG_BASE = "/kn/bao-gia/cau-hinh";
export type ConfigRoute =
  | { kind: "schedule"; edit: boolean }
  | { kind: "reminder"; edit: boolean }
  | { kind: "exception-new" }
  | { kind: "exception"; day: string }
  | { kind: "threshold-new" }
  | {
      kind: "threshold";
      scope: "default" | "group" | "item";
      code: string;
      edit: boolean;
    };

export function configBase(path: string): string {
  return (
    /^\/kn\/phong-ban\/[^/]+\/bao-gia-lich(?:\/|$)/
      .exec(path)?.[0]
      .replace(/\/$/, "") ?? PRICE_CONFIG_BASE
  );
}

/** Prefix phạm vi phân biệt mã nhóm/mặt hàng với thao tác của route. */
export function thresholdPath(
  scope: "default" | "group" | "item",
  code = "",
): string {
  return scope === "default"
    ? "nguong/mac-dinh"
    : `nguong/${scope}/${encodeURIComponent(code)}`;
}

export function isCalendarDay(value: string): boolean {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
  const day = new Date(`${value}T00:00:00Z`);
  return (
    Number.isFinite(day.getTime()) && day.toISOString().slice(0, 10) === value
  );
}

export function parseConfigRoute(parts: readonly string[]): ConfigRoute | null {
  const [section, action, code, edit] = parts;
  if (
    (section === "lich" || section === "nhac") &&
    (parts.length === 1 || (parts.length === 2 && action === "sua"))
  ) {
    return {
      kind: section === "lich" ? "schedule" : "reminder",
      edit: action === "sua",
    };
  }
  if (section === "ngoai-le" && parts.length === 2) {
    if (action === "them-moi") return { kind: "exception-new" };
    if (isCalendarDay(action ?? "")) return { kind: "exception", day: action! };
  }
  if (section === "nguong") {
    if (parts.length === 2 && action === "them-moi")
      return { kind: "threshold-new" };
    if (
      action === "mac-dinh" &&
      (parts.length === 2 || (parts.length === 3 && code === "sua"))
    ) {
      return {
        kind: "threshold",
        scope: "default",
        code: "",
        edit: code === "sua",
      };
    }
    if (
      (action === "group" || action === "item") &&
      (parts.length === 3 || (parts.length === 4 && edit === "sua")) &&
      (action === "group" ? /^[a-z][a-z0-9_]{0,39}$/ : /^MH[0-9]{5}$/).test(
        code ?? "",
      )
    ) {
      return {
        kind: "threshold",
        scope: action,
        code: code!,
        edit: edit === "sua",
      };
    }
  }
  return null;
}
