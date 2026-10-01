"use client";

// Thành phần dùng chung của màn Mẫu báo cáo (FN-AD-04.06): nhãn trạng thái, lỗi máy chủ theo trường (cột,
// công thức, thao tác), nhãn nhật ký, dữ liệu mẫu cho "Xem trước màn hình nhập liệu".
import type { ApiProblem } from "@/api/problem";
import { Icon } from "@/components/Icon";
import { StatusPill, type PillTone } from "@/components/patterns/list";
import { columnLetter } from "@/kn/grid/letters";
import type { GridCommune, GridValues } from "@/kn/grid/model";
import { rowKeyOf } from "@/kn/grid/model";
import type { FormDefinition, FormLogEntry, FormVersion } from "./types";
import { REPORT_TYPE_LABEL, VERSION_STATUS_LABEL, type FormReportType } from "./types";

/** Tên phòng ban hiển thị (tạm theo mã P2; Q-02 chờ khách). */
const DEPT_NAMES: Record<string, string> = {
  trong_trot: "Phòng Trồng trọt",
  chan_nuoi_thuy_san: "Phòng Chăn nuôi - Thủy sản",
  ke_hoach_tai_chinh: "Phòng Kế hoạch - Tài chính",
};

export function deptName(code: string): string {
  return DEPT_NAMES[code] ?? code;
}

const VERSION_TONE: Record<FormVersion["status"], PillTone> = { draft: "neutral", proposed: "warn", published: "ok", retired: "bad" };

export function VersionPill({ status }: { status: FormVersion["status"] }) {
  return <StatusPill tone={VERSION_TONE[status]}>{VERSION_STATUS_LABEL[status]}</StatusPill>;
}

export const LOG_EVENT_LABEL: Record<FormLogEntry["event"], string> = {
  template_created: "Tạo mẫu",
  template_updated: "Sửa thông tin mẫu",
  template_retired: "Ngừng dùng mẫu",
  draft_created: "Tạo phiên bản nháp",
  draft_saved: "Lưu nháp",
  draft_ops_applied: "Sửa cấu trúc lưới",
  imported: "Nạp từ Excel",
  seeded: "Khởi tạo từ bộ 14 mẫu",
  proposed: "Đề nghị phát hành",
  proposal_withdrawn: "Rút đề nghị",
  published: "Phát hành phiên bản",
  version_retired: "Ngừng dùng phiên bản",
  assigned: "Gán loại báo cáo",
  assignment_revoked: "Bỏ gán loại báo cáo",
};

function short(v: unknown): string | null {
  if (v === null || v === undefined) return null;
  if (typeof v === "string") return v.length > 60 ? `${v.slice(0, 57)}…` : v;
  if (typeof v === "number" || typeof v === "boolean") return String(v);
  if (Array.isArray(v)) return `${v.length} mục`;
  return "…";
}

const rt = (v: unknown) => (typeof v === "string" ? (REPORT_TYPE_LABEL[v as FormReportType] ?? v) : null);

/** Nhật ký → dòng của LogPanel (P6 pattern Detail). */
export function toLogEntries(items: FormLogEntry[]) {
  return items.map((e) => ({
    event: `${LOG_EVENT_LABEL[e.event] ?? e.event}${e.version ? ` · v${e.version}` : ""}`,
    actor_display: e.actor_display ?? e.actor_ref,
    at: e.at,
    changes: e.changes.slice(0, 6).map((c) => (c.field === "report_type" ? { field: "loại báo cáo", old: rt(c.old), new: rt(c.new) } : { field: c.field, old: short(c.old), new: short(c.new) })),
    reason: e.reason,
  }));
}

/** "definition.columns[10].formula" → "Cột K · công thức"; "ops[2]" → "Thao tác 3". */
export function fieldLabel(field: string | undefined, def?: FormDefinition | null): string {
  if (!field) return "";
  const f = field.replace(/^definition\./, "");
  const col = /^columns\[(\d+)\](?:\.(\w+))?$/.exec(f);
  if (col) {
    const L = def?.columns[Number(col[1])]?.letter ?? columnLetter(Number(col[1]) + 1);
    const part: Record<string, string> = { formula: "công thức", letter: "chữ cái", title: "tiêu đề", kind: "loại cột", dtype: "kiểu dữ liệu", catalog: "danh mục chọn", period_block: "khối kỳ", period_slot: "kỳ", fixed_source: "nguồn hệ thống", min: "giới hạn dưới", max: "giới hạn trên", decimals: "số chữ số", group: "nhóm cột", total: "dòng tổng" };
    return `Cột ${L}${col[2] ? ` · ${part[col[2]] ?? col[2]}` : ""}`;
  }
  const op = /^ops\[(\d+)\]$/.exec(f);
  if (op) return `Thao tác ${Number(op[1]) + 1}`;
  const m = /^header\.merges\[(\d+)\]$/.exec(f);
  if (m) return `Vùng gộp ${Number(m[1]) + 1}`;
  const r = /^rules\[(\d+)\](?:\.(\w+))?$/.exec(f);
  if (r) return `Ràng buộc ${def?.rules[Number(r[1])]?.id ?? Number(r[1]) + 1}${r[2] ? ` · ${r[2]}` : ""}`;
  const pb = /^period_blocks\[(\d+)\]/.exec(f);
  if (pb) return `Khối kỳ ${def?.period_blocks[Number(pb[1])]?.id ?? Number(pb[1]) + 1}`;
  const named: Record<string, string> = { effective_from: "Áp dụng từ kỳ", effective_label: "Nhãn kỳ áp dụng", reason: "Lý do", code: "Mã mẫu", name: "Tên mẫu", freeze_through: "Cố định tới cột", title: "Tên mẫu", report_type: "Loại báo cáo" };
  return named[f] ?? f;
}

/** Lỗi máy chủ: thông báo tiếng Việt theo mã + chi tiết + lỗi từng trường (cột/công thức/thao tác) + mã tra cứu. */
export function FormProblem({ problem, def, onClose }: { problem: ApiProblem; def?: FormDefinition | null; onClose?: () => void }) {
  const detail = problem.problem.detail && problem.problem.detail !== problem.message ? problem.problem.detail : null;
  return (
    <div className="alert alert-bad" role="alert">
      <Icon name="alert" />
      <div style={{ flex: 1 }}>
        <b>{problem.message}</b>
        {detail ? <div className="small">{detail}</div> : null}
        {problem.fieldErrors.length ? (
          <ul style={{ margin: "6px 0 0 18px" }}>
            {problem.fieldErrors.map((e, i) => (
              <li key={`${e.field ?? ""}-${i}`}>
                {e.field ? <strong>{fieldLabel(e.field, def)}: </strong> : null}
                {e.reason}
              </li>
            ))}
          </ul>
        ) : null}
        <div className="small" style={{ marginTop: 6 }}>
          Mã lỗi <code>{problem.code}</code>
          {problem.traceId ? (
            <>
              {" "}
              · Mã tra cứu <code>{problem.traceId}</code>
            </>
          ) : null}
        </div>
      </div>
      {onClose ? (
        <button type="button" className="btn btn-o btn-sm" onClick={onClose}>
          Đóng
        </button>
      ) : null}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Xem trước: xã mẫu + số liệu mẫu cố định (không phải số thật)
// ---------------------------------------------------------------------------

/** Xã mẫu cho xem trước (mã, tên minh họa — ĐVHC thật lấy từ P2 khi nhập thật). */
export const PREVIEW_COMMUNES: GridCommune[] = [
  { code: "91301", name: "Mỹ Hòa Hưng", region: "KV883", regionName: "Long Xuyên" },
  { code: "91302", name: "Bình Đức", region: "KV883", regionName: "Long Xuyên" },
  { code: "91310", name: "Vĩnh Tế", region: "KV884", regionName: "Châu Đốc" },
];

/** Số liệu mẫu tất định cho mọi ô nhập số (mỗi xã khác nhau), Tỉnh giao cho cột Tỉnh giao. */
export function sampleData(def: FormDefinition, communes: readonly GridCommune[]): { values: GridValues; system: GridValues } {
  const values: GridValues = {};
  const system: GridValues = {};
  const inds = def.rows.indicators.length ? def.rows.indicators.slice(0, 3).map((i) => i.code) : [""];
  communes.forEach((c, ci) => {
    for (const ind of inds) {
      const key = rowKeyOf(c.code, ind || undefined);
      const v: Record<string, string> = {};
      const s: Record<string, string> = {};
      def.columns.forEach((col, i) => {
        const base = ((ci + 1) * 7 + (i % 9) * 3) % 40;
        if (col.kind === "provincial" && (col.dtype === "number" || col.dtype === "integer")) s[col.letter] = String(100 + ci * 20);
        if ((col.kind === "input" || col.kind === "per_period") && !col.inactive) {
          if (col.dtype === "number") v[col.letter] = `${base + 1}.5`;
          else if (col.dtype === "integer") v[col.letter] = String(base + 1);
          else if (col.dtype === "date") v[col.letter] = `2026-${String(9 + (ci % 3)).padStart(2, "0")}-${String(10 + i % 15).padStart(2, "0")}`;
        }
      });
      values[key] = v;
      if (Object.keys(s).length) system[key] = s;
    }
  });
  return { values, system };
}
