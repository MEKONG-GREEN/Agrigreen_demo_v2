"use client";

// Gói ORGUI — hộp thoại "Tạo tổ chức" trên /quan-tri/to-chuc (Admin POST /v1/orgs, MFA).
// Trường đổi theo loại; HTX chuyển sang /quan-tri/neo-htx (ADR-0052). Idempotency-Key sinh MỘT lần
// mỗi lần mở hộp thoại và dùng lại khi gửi lại (sửa lỗi 409/422 hoặc lỗi mạng) — xem docs/kn/gia-dinh/ORGUI.md.
import Link from "next/link";
import { useId, useMemo, useState, type FormEvent } from "react";
import { unwrap } from "@/api/client";
import { newIdempotencyKey } from "@/api/idempotency";
import { collectPages } from "@/api/pagination";
import { ApiProblem } from "@/api/problem";
import type { Org, OrgKind } from "@/api/types";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { ProblemAlert } from "@/components/ProblemAlert";
import { useAsync } from "@/lib/useAsync";
import {
  buildOrgInput,
  CREATE_KIND_ORDER,
  EMPTY_FORM,
  fieldErrorsFromProblem,
  KIND_RULES,
  parentOptions,
  unitOptions,
  validateForm,
  vnToday,
  type FieldErrors,
  type OrgForm,
} from "./orgCreate";

export function OrgCreateDialog({ orgs, onClose, onCreated }: { orgs: readonly Org[]; onClose: () => void; onCreated: (org: Org) => void }) {
  const { adminApi, api } = useAuth();
  const id = useId();
  const formId = `${id}-form`;
  const [form, setForm] = useState<OrgForm>(EMPTY_FORM);
  const [errs, setErrs] = useState<FieldErrors>({});
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  // Một khóa cho mỗi lần mở hộp thoại: gửi lại sau lỗi dùng đúng khóa này.
  const [idemKey] = useState(newIdempotencyKey);
  const day = useMemo(() => vnToday(), []);

  const rule = KIND_RULES[form.kind];
  const parents = useMemo(() => parentOptions(form.kind, orgs), [form.kind, orgs]);
  const parent = form.parent_ref ? (orgs.find((o) => o.ref === form.parent_ref) ?? null) : null;
  // ĐVHC: tỉnh → danh sách tỉnh; xã → xã thuộc tỉnh của cấp trên. Tra theo ngày hiệu lực hôm nay.
  const unitParent = rule.unit === "commune" ? (parent?.admin_unit_code ?? null) : null;
  const needUnits = rule.unit === "province" || (rule.unit === "commune" && !!unitParent);
  const units = useAsync(
    () =>
      needUnits
        ? collectPages(
            (cursor) =>
              unwrap(
                api.GET("/v1/masterdata/admin-units", {
                  params: {
                    query: {
                      parent: unitParent ?? undefined,
                      at: day,
                      cursor,
                      limit: 200,
                    },
                  },
                }),
              ),
            2000,
          )
        : Promise.resolve([]),
    [api, needUnits, unitParent, day],
  );
  const unitChoices = useMemo(() => unitOptions(form.kind, units.data ?? [], parent, day), [form.kind, units.data, parent, day]);

  const set = <K extends keyof OrgForm>(k: K, v: OrgForm[K]) => {
    setForm((f) => {
      const next = { ...f, [k]: v };
      if (k === "kind") {
        next.parent_ref = "";
        next.admin_unit_code = "";
      }
      if (k === "parent_ref" && KIND_RULES[next.kind].unit === "commune") next.admin_unit_code = "";
      return next;
    });
    setErrs((e) => ({ ...e, [k]: undefined }));
  };

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    const local = validateForm(form, orgs);
    setErrs(local);
    setProblem(null);
    if (Object.keys(local).length) return;
    setBusy(true);
    try {
      const res = await unwrap(
        adminApi.POST("/v1/orgs", {
          params: { header: { "Idempotency-Key": idemKey } },
          body: buildOrgInput(form),
        }),
      );
      onCreated(res.org as Org);
    } catch (err) {
      const p = err instanceof ApiProblem ? err : null;
      const fe = p ? fieldErrorsFromProblem(p) : null;
      if (fe) setErrs(fe);
      else setProblem(p);
    } finally {
      setBusy(false);
    }
  };

  const err = (k: keyof FieldErrors) =>
    errs[k] ? (
      <span className="err small" id={`${id}-${k}-err`} role="alert">
        {errs[k]}
      </span>
    ) : null;
  const a11y = (k: keyof FieldErrors) => ({
    "aria-invalid": !!errs[k],
    "aria-describedby": errs[k] ? `${id}-${k}-err` : undefined,
  });

  return (
    <Dialog
      title="Tạo tổ chức"
      onClose={onClose}
      actions={
        rule.redirect ? null : (
          <button type="submit" form={formId} className="btn btn-p btn-sm" disabled={busy}>
            {busy ? "Đang tạo…" : "Tạo"}
          </button>
        )
      }
    >
      <form id={formId} className="stack" style={{ gap: 10 }} aria-label="Tạo tổ chức" noValidate onSubmit={(e) => void submit(e)}>
        <div className="field">
          <label htmlFor={`${id}-kind`}>Loại tổ chức</label>
          <select id={`${id}-kind`} className="input" value={form.kind} onChange={(e) => set("kind", e.target.value as OrgKind)} {...a11y("kind")}>
            {CREATE_KIND_ORDER.map((k) => (
              <option key={k} value={k}>
                {KIND_RULES[k].label}
              </option>
            ))}
          </select>
        </div>

        {rule.redirect ? (
          <div className="panel stack" style={{ gap: 6 }} data-testid="org-create-coop-redirect">
            <b>HTX / THT không tạo ở đây</b>
            <span className="small">
              HTX mới đi qua luồng HTX tạm neo theo mã số thuế (ADR-0052, quyết định D2) để dữ liệu đồng sở hữu MKG + HTX có neo ngay từ đầu.
            </span>
            <Link href={rule.redirect} className="btn btn-o btn-sm" style={{ alignSelf: "flex-start" }}>
              Mở Neo HTX theo mã số thuế
            </Link>
          </div>
        ) : (
          <>
            <div className="field">
              <label htmlFor={`${id}-code`}>Mã tổ chức</label>
              <input id={`${id}-code`} className="input" value={form.code} maxLength={40} onChange={(e) => set("code", e.target.value)} {...a11y("code")} />
              <span className="small mute">1–40 ký tự: chữ không dấu, số, dấu . _ -. Mã không tái sử dụng.</span>
              {err("code")}
            </div>
            <div className="field">
              <label htmlFor={`${id}-name`}>Tên tổ chức</label>
              <input id={`${id}-name`} className="input" value={form.name} maxLength={200} onChange={(e) => set("name", e.target.value)} {...a11y("name")} />
              {err("name")}
            </div>
            {rule.parents.length > 0 ? (
              <div className="field">
                <label htmlFor={`${id}-parent`}>Tổ chức cấp trên</label>
                <select
                  id={`${id}-parent`}
                  className="input"
                  value={form.parent_ref}
                  onChange={(e) => set("parent_ref", e.target.value)}
                  {...a11y("parent_ref")}
                >
                  <option value="">{rule.root ? "— Không (đứng ở gốc) —" : "— Chọn —"}</option>
                  {parents.map((o) => (
                    <option key={o.ref} value={o.ref}>
                      {o.name} ({o.code})
                    </option>
                  ))}
                </select>
                <span className="small mute">
                  Chỉ hiện tổ chức đang hoạt động thuộc loại cấp trên hợp lệ: {rule.parents.map((k) => KIND_RULES[k].label).join(", ")}.
                </span>
                {err("parent_ref")}
              </div>
            ) : (
              err("parent_ref")
            )}
            {rule.unit === "province" || rule.unit === "commune" ? (
              <div className="field">
                <label htmlFor={`${id}-unit`}>{rule.unit === "province" ? "Tỉnh/thành (ĐVHC)" : "Xã/phường (ĐVHC)"}</label>
                <select
                  id={`${id}-unit`}
                  className="input"
                  value={form.admin_unit_code}
                  disabled={rule.unit === "commune" && !unitParent}
                  onChange={(e) => set("admin_unit_code", e.target.value)}
                  {...a11y("admin_unit_code")}
                >
                  <option value="">{rule.unit === "commune" && !unitParent ? "— Chọn cấp trên trước —" : "— Chọn —"}</option>
                  {unitChoices.map((u) => (
                    <option key={u.code} value={u.code}>
                      {u.name} ({u.code})
                    </option>
                  ))}
                </select>
                <span className="small mute">
                  Chỉ đơn vị còn hiệu lực ngày {day}
                  {rule.unit === "commune" ? ", thuộc tỉnh của cấp trên" : ""}.
                </span>
                {units.error ? <ProblemAlert problem={units.error} onRetry={units.reload} /> : null}
                {err("admin_unit_code")}
              </div>
            ) : null}
            {rule.unit === "any" ? (
              <div className="field">
                <label htmlFor={`${id}-unit`}>Mã ĐVHC (tuỳ chọn, tỉnh hoặc xã)</label>
                <input
                  id={`${id}-unit`}
                  className="input"
                  value={form.admin_unit_code}
                  onChange={(e) => set("admin_unit_code", e.target.value)}
                  {...a11y("admin_unit_code")}
                />
                {err("admin_unit_code")}
              </div>
            ) : null}
            {err("kind")}
            {problem ? <ProblemAlert problem={problem} /> : null}
          </>
        )}
      </form>
    </Dialog>
  );
}
