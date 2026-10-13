"use client";

// FN-AD-04.01 — Danh mục KHTC (BRD KHTC v2.2; W11.61, pf/extension). Cột trái: 7 loại danh mục kèm số lượng; bảng mục
// của loại đang chọn; Admin thêm, sửa, ngừng dùng/dùng lại (mục đã dùng không xóa), chọn mẫu áp dụng mặc định (người
// ký, nơi nhận, căn cứ — một mẫu chỉ một người ký mặc định đang dùng). Cấp Tỉnh chỉ xem. Nhập Excel danh mục: hoãn.
import { useState } from "react";
import { Dialog } from "@/components/Dialog";
import { ProblemAlert } from "@/components/ProblemAlert";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, ReadOnlyNotice, useAction, useLoad } from "@/kn/cycle/common";
import {
  CATALOG_KINDS,
  KIND_ATTRS,
  KIND_LABEL,
  TEMPLATED,
  TEMPLATES,
  TEMPLATE_LABEL,
  dmy,
  useKhtcApi,
  useKhtcRights,
  type KhtcCatalogItem,
  type KhtcCatalogKind,
} from "./api";

type Edit = { mode: "new" } | { mode: "edit"; item: KhtcCatalogItem };

export function KhtcCatalogScreen() {
  const api = useKhtcApi();
  const rights = useKhtcRights();
  const [kind, setKind] = useState<KhtcCatalogKind>("unit");
  const [retired, setRetired] = useState(false);
  const [edit, setEdit] = useState<Edit | null>(null);
  const s = useLoad(() => api.catalog(kind, retired), [kind, retired]);
  const act = useAction();

  const toggle = async (it: KhtcCatalogItem) => {
    const r = await act.run(
      () => api.reviseCatalog(it.kind, it.code, { name: it.name, sort_order: it.sort_order, active: !it.active, default_for: it.default_for, attrs: it.attrs }),
      it.active ? `Đã ngừng dùng ${it.code}` : `Đã dùng lại ${it.code}`,
    );
    if (r) s.reload();
  };

  return (
    <div>
      <PageHeader
        title="Danh mục KHTC"
        actions={
          rights.catalog ? (
            <button type="button" className="btn btn-p btn-sm" onClick={() => setEdit({ mode: "new" })}>
              Thêm
            </button>
          ) : null
        }
      >
        Loại báo cáo, ĐVT, lĩnh vực, nguồn kinh phí, người ký, nơi nhận, căn cứ (mặt hàng giá cả quản lý ở Báo giá). Đổi mặc định chỉ áp dụng cho bản soạn mới.
      </PageHeader>
      {!rights.catalog ? <ReadOnlyNotice>Chỉ xem — Admin quản lý Danh mục KHTC.</ReadOnlyNotice> : null}
      {act.problem ? <ProblemAlert problem={act.problem} /> : null}
      {act.note ? (
        <div className="alert alert-info" role="status">
          {act.note}
        </div>
      ) : null}
      <LoadState s={s}>
        {(d) => (
          <div className="grid2" style={{ gridTemplateColumns: "220px 1fr", alignItems: "start" }}>
            <div className="card" role="tablist" aria-label="Loại danh mục">
              {CATALOG_KINDS.map((k) => {
                const c = d.counts.find((x) => x.kind === k);
                return (
                  <button
                    key={k}
                    type="button"
                    role="tab"
                    aria-selected={k === kind}
                    className={k === kind ? "btn btn-p btn-sm" : "btn btn-o btn-sm"}
                    style={{ display: "flex", justifyContent: "space-between", width: "100%", marginBottom: 4 }}
                    onClick={() => setKind(k)}
                  >
                    <span>{KIND_LABEL[k]}</span>
                    <span>{c ? c.active : 0}</span>
                  </button>
                );
              })}
            </div>
            <div className="card">
              <label className="row small">
                <input type="checkbox" checked={retired} onChange={(e) => setRetired(e.target.checked)} /> Hiện cả mục đã ngừng dùng
              </label>
              <div className="tbl-wrap">
                <table className="dt">
                  <caption className="sr-only">{KIND_LABEL[kind]}</caption>
                  <thead>
                    <tr>
                      <th scope="col">Mã</th>
                      <th scope="col">Tên</th>
                      {(KIND_ATTRS[kind] ?? []).map((a) => (
                        <th scope="col" key={a.key}>
                          {a.label}
                        </th>
                      ))}
                      {TEMPLATED.includes(kind) ? <th scope="col">Mặc định cho mẫu</th> : null}
                      <th scope="col">Trạng thái</th>
                      <th scope="col">Cập nhật</th>
                      {rights.catalog ? <th scope="col">Thao tác</th> : null}
                    </tr>
                  </thead>
                  <tbody>
                    {d.items.length ? (
                      d.items.map((it) => (
                        <tr key={it.code}>
                          <td>{it.code}</td>
                          <td>{it.name}</td>
                          {(KIND_ATTRS[kind] ?? []).map((a) => (
                            <td key={a.key}>{it.attrs[a.key] ?? "—"}</td>
                          ))}
                          {TEMPLATED.includes(kind) ? <td>{it.default_for.map((t) => TEMPLATE_LABEL[t] ?? t).join(", ") || "—"}</td> : null}
                          <td>{it.active ? <StatusPill tone="ok">Đang dùng</StatusPill> : <StatusPill>Ngừng dùng</StatusPill>}</td>
                          <td className="small">
                            v{it.version} · {dmy(it.recorded_at)}
                          </td>
                          {rights.catalog ? (
                            <td className="row">
                              <button type="button" className="btn btn-o btn-sm" onClick={() => setEdit({ mode: "edit", item: it })}>
                                Sửa
                              </button>
                              <button type="button" className="btn btn-o btn-sm" disabled={act.busy} onClick={() => toggle(it)}>
                                {it.active ? "Ngừng dùng" : "Dùng lại"}
                              </button>
                            </td>
                          ) : null}
                        </tr>
                      ))
                    ) : (
                      <tr>
                        <td colSpan={8} className="empty">
                          Chưa có mục nào.
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          </div>
        )}
      </LoadState>
      {edit ? (
        <CatalogDialog
          kind={kind}
          edit={edit}
          onClose={() => setEdit(null)}
          onSaved={() => {
            setEdit(null);
            s.reload();
          }}
        />
      ) : null}
    </div>
  );
}

function CatalogDialog({ kind, edit, onClose, onSaved }: { kind: KhtcCatalogKind; edit: Edit; onClose: () => void; onSaved: () => void }) {
  const api = useKhtcApi();
  const it = edit.mode === "edit" ? edit.item : null;
  const [code, setCode] = useState(it?.code ?? "");
  const [name, setName] = useState(it?.name ?? "");
  const [sort, setSort] = useState(String(it?.sort_order ?? 0));
  const [attrs, setAttrs] = useState<Record<string, string>>({ ...(it?.attrs ?? {}) });
  const [defaults, setDefaults] = useState<string[]>(it?.default_for ?? []);
  const act = useAction();
  const fields = KIND_ATTRS[kind] ?? [];
  const missing = !name.trim() || (!it && !code.trim()) || fields.some((f) => f.required && !attrs[f.key]?.trim());

  const save = async () => {
    const body = { code, name, sort_order: Number.parseInt(sort, 10) || 0, active: it?.active ?? true, default_for: defaults, attrs };
    const r = await act.run(() => (it ? api.reviseCatalog(kind, it.code, body) : api.createCatalog(kind, body)));
    if (r) onSaved();
  };

  return (
    <Dialog
      title={`${it ? "Sửa" : "Thêm"} ${KIND_LABEL[kind].toLowerCase()}`}
      onClose={onClose}
      actions={
        <button type="button" className="btn btn-p btn-sm" disabled={missing || act.busy} onClick={save}>
          Lưu
        </button>
      }
    >
      {act.problem ? <ProblemAlert problem={act.problem} /> : null}
      <div className="field">
        <label htmlFor="kc-code" className="req">
          Mã
        </label>
        <input id="kc-code" className="input" value={code} disabled={!!it} onChange={(e) => setCode(e.target.value)} />
      </div>
      <div className="field">
        <label htmlFor="kc-name" className="req">
          {kind === "signer" ? "Họ tên" : "Tên"}
        </label>
        <input id="kc-name" className="input" value={name} onChange={(e) => setName(e.target.value)} />
      </div>
      {fields.map((f) => (
        <div className="field" key={f.key}>
          <label htmlFor={`kc-${f.key}`} className={f.required ? "req" : undefined}>
            {f.label}
          </label>
          <input id={`kc-${f.key}`} className="input" value={attrs[f.key] ?? ""} onChange={(e) => setAttrs({ ...attrs, [f.key]: e.target.value })} />
        </div>
      ))}
      <div className="field">
        <label htmlFor="kc-sort">Thứ tự</label>
        <input id="kc-sort" className="input" inputMode="numeric" value={sort} onChange={(e) => setSort(e.target.value)} />
      </div>
      {TEMPLATED.includes(kind) ? (
        <fieldset className="field">
          <legend className="lbl">Mặc định cho mẫu</legend>
          {TEMPLATES.map((t) => (
            <label key={t} className="row small">
              <input
                type="checkbox"
                checked={defaults.includes(t)}
                onChange={(e) => setDefaults(e.target.checked ? [...defaults, t] : defaults.filter((x) => x !== t))}
              />{" "}
              {TEMPLATE_LABEL[t]}
            </label>
          ))}
        </fieldset>
      ) : null}
    </Dialog>
  );
}
