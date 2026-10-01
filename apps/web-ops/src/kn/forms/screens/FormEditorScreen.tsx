"use client";

// FN-AD-04.06 B2–B6 — Trình thiết kế mẫu (Hình 27): lưới đúng bố cục Excel (hàng chữ cái, tiêu đề gộp, dòng loại
// cột), thanh công cụ áp 16 thao tác qua máy chủ (POST …/ops — cả lô hoặc không gì, lỗi hiện theo cột/công thức),
// Thuộc tính mẫu / Thuộc tính cột, xem trước màn nhập với số liệu mẫu (tính trên máy chủ), Lưu nháp → Đề nghị
// phát hành → Phát hành bởi NGƯỜI KHÁC (R-56) → Ngừng dùng; tải mẫu Excel.
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import type { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { Icon } from "@/components/Icon";
import { BackLink } from "@/components/patterns/detail";
import { Loading } from "@/components/ui";
import { saveBlob } from "@/lib/files";
import { formatDateTime } from "@/lib/format";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import { useBreadcrumbTail } from "@/shell/Breadcrumb";
import { FormEntry } from "@/kn/grid/FormEntry";
import { FormGrid } from "@/kn/grid/FormGrid";
import { columnIndex, columnLetter, rectString, type Rect } from "@/kn/grid/letters";
import type { GridValues } from "@/kn/grid/model";
import { adminEvaluator, applyFormOps, downloadTemplateXlsx, getForm, getFormVersion, newFormVersion, proposeVersion, publishVersion, retireVersion, saveDraft, withdrawVersion } from "../api";
import { extendRect, insertColumnOps, mergeAt, moveColumnOps, moveIndicatorOps, periodSlotChange, withSeason } from "../designer";
import {
  COMPLETENESS_LABEL,
  DTYPE_LABEL,
  KIND_LABEL,
  LEVEL_LABEL,
  REPORT_TYPE_LABEL,
  type FormColumn,
  type FormDefinition,
  type FormOp,
  type FormRule,
  type FormTotalDef,
  type FormVersion,
} from "../types";
import { deptName, FormProblem, PREVIEW_COMMUNES, sampleData, VersionPill } from "../ui";
import { ReasonDialog } from "./FormDetailScreen";
import { formsBase } from "./FormListScreen";

type DialogKind = "indicators" | "rules" | "period" | "groups" | "totals" | null;

export function FormEditorScreen({ dept, id, v }: { dept: string; id: string; v: number }) {
  const { adminApi, api, principal } = useAuth();
  const router = useRouter();
  const base = formsBase(dept);
  const loaded = useAsync(() => getFormVersion(adminApi, id, v), [adminApi, id, v]);
  const detail = useAsync(() => getForm(adminApi, id), [adminApi, id]);
  const [override, setOverride] = useState<FormVersion | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [sel, setSel] = useState<string | null>(null);
  const [anchor, setAnchor] = useState<{ row: number; col: number } | null>(null);
  const [hsel, setHsel] = useState<Rect | null>(null);
  const [preview, setPreview] = useState(false);
  const [dialog, setDialog] = useState<DialogKind>(null);
  const [retiring, setRetiring] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [fx, setFx] = useState(0);
  const [warn, setWarn] = useState<string | null>(null);

  const tpl = loaded.data?.template;
  const version = override && override.version === v ? override : loaded.data?.version;
  const def = version?.definition ?? null;
  useBreadcrumbTail(tpl ? [{ label: `${tpl.code} · ${tpl.name}`, href: `${base}/${id}` }, { label: `v${v}` }] : []);

  if (loaded.loading && !loaded.data) return <Loading />;
  if (loaded.error && !loaded.data) return <FormProblem problem={loaded.error} />;
  if (!tpl || !version || !def) return null;

  const editable = version.status === "draft" && tpl.status === "active";
  const assignments = (detail.data?.assignments ?? []).filter((a) => a.status === "active");
  const selCol = sel ? def.columns.find((c) => c.letter === sel) ?? null : null;
  const me = principal?.subject ?? "";
  const nameOf = (ref: string | null) => (ref ? (ref === me ? "bạn" : ref.replace(/^mock-/, "")) : "—");

  const run = async (fn: () => Promise<FormVersion>, msg?: string) => {
    setBusy(true);
    setProblem(null);
    setNotice(null);
    try {
      const r = await fn();
      setOverride(r);
      detail.reload();
      if (msg) setNotice(msg);
      return r;
    } catch (e) {
      setProblem(toApiProblem(e));
      return null;
    } finally {
      setBusy(false);
    }
  };
  const ops = (list: FormOp[], msg?: string) => run(() => applyFormOps(adminApi, id, v, list), msg);

  const lastLetter = def.columns.at(-1)!.letter;
  const at = sel ?? lastLetter;
  const singleHeader = hsel && hsel.c1 === hsel.c2 && hsel.r1 === hsel.r2 ? hsel : null;
  const mergeOfSel = hsel ? mergeAt(def, hsel.r1, hsel.c1) : null;
  const headerText = singleHeader ? (def.header.cells.find((c) => c.row === singleHeader.r1 && columnIndex(c.letter) === singleHeader.c1)?.text ?? "") : "";

  const download = async () => {
    setProblem(null);
    try {
      const f = await downloadTemplateXlsx(api, id, v, `${tpl.code}-v${v}.xlsx`);
      saveBlob(f.blob, f.name);
    } catch (e) {
      setProblem(toApiProblem(e));
    }
  };

  return (
    <>
      <BackLink href={`${base}/${id}`}>
        {tpl.code} · {tpl.name}
      </BackLink>
      <div className="pg-head">
        <h1>
          Chỉnh sửa cấu trúc mẫu {tpl.code} <VersionPill status={version.status} />
        </h1>
        <div className="row">
          <button type="button" className="btn btn-o" onClick={() => void download()}>
            <Icon name="download" size={16} /> Tải mẫu Excel
          </button>
          <Link className="btn btn-o" href={`${base}/${id}`}>
            <Icon name="clock" size={16} /> Lịch sử phiên bản
          </Link>
        </div>
      </div>
      {problem ? <FormProblem problem={problem} def={def} onClose={() => setProblem(null)} /> : null}
      {warn ? (
        <div className="alert alert-warn" role="alert" style={{ marginBottom: 12 }}>
          <Icon name="alert" />
          <span>{warn}</span>
        </div>
      ) : null}
      {notice ? (
        <div className="alert alert-info" role="status" style={{ marginBottom: 12 }}>
          <Icon name="check" />
          <span>{notice}</span>
        </div>
      ) : null}
      <div className="fd-grid">
        <div className="stack" style={{ minWidth: 0 }}>
          <section className="card fd-main" aria-labelledby="fd-title">
            <h2 id="fd-title">
              {def.title} (đúng bố cục Excel) · v{version.version}
            </h2>
            {editable ? (
              <div className="fd-toolbar" role="toolbar" aria-label="Thao tác lưới">
                <button type="button" className="btn btn-sm" disabled={busy} onClick={() => void ops(insertColumnOps(at, "before")).then((r) => r && setSel(at))}>
                  + Cột trước
                </button>
                <button
                  type="button"
                  className="btn btn-sm"
                  disabled={busy}
                  onClick={() => void ops(insertColumnOps(at, "after")).then((r) => r && setSel(columnLetter(columnIndex(at) + 1)))}
                >
                  + Cột sau
                </button>
                <button type="button" className="btn btn-sm" disabled={busy || !sel} onClick={() => void ops([{ op: "delete_column", letter: sel! }]).then((r) => r && setSel(null))}>
                  − Xóa cột
                </button>
                {([-1, 1] as const).map((dir) => (
                  <button
                    key={dir}
                    type="button"
                    className="btn btn-sm"
                    disabled={busy || !sel}
                    onClick={() => {
                      const m = moveColumnOps(def, sel!, dir);
                      setWarn("error" in m ? m.error : null);
                      if ("ops" in m) void ops(m.ops).then((r) => r && setSel(columnLetter(columnIndex(sel!) + dir)));
                    }}
                  >
                    {dir < 0 ? "← Chuyển cột" : "Chuyển cột →"}
                  </button>
                ))}
                <button type="button" className="btn btn-sm" disabled={busy} onClick={() => void ops([{ op: "insert_header_row", row: hsel?.r1 ?? 1, side: "before" }])}>
                  + Dòng tiêu đề
                </button>
                <button
                  type="button"
                  className="btn btn-sm"
                  disabled={busy || !hsel || def.header.rows < 2}
                  onClick={() => void ops([{ op: "delete_header_row", row: hsel!.r1 }]).then(() => setHsel(null))}
                >
                  − Xóa dòng tiêu đề
                </button>
                <button type="button" className="btn btn-sm" disabled={busy} onClick={() => setDialog("indicators")}>
                  ± Dòng chỉ tiêu
                </button>
                <button
                  type="button"
                  className="btn btn-sm"
                  disabled={busy || !hsel || (hsel.c1 === hsel.c2 && hsel.r1 === hsel.r2)}
                  onClick={() => void ops([{ op: "merge", range: rectString(hsel!) }])}
                >
                  Gộp ô
                </button>
                <button type="button" className="btn btn-sm" disabled={busy || !mergeOfSel} onClick={() => void ops([{ op: "unmerge", range: mergeOfSel! }])}>
                  Tách ô
                </button>
                <button
                  type="button"
                  className="btn btn-sm"
                  disabled={busy || !selCol}
                  onClick={() => setFx((n) => n + 1)}
                >
                  ƒx Công thức
                </button>
                <button type="button" className="btn btn-sm" disabled={busy} onClick={() => setDialog("period")}>
                  Khối kỳ
                </button>
                <button type="button" className="btn btn-sm" disabled={busy} onClick={() => setDialog("rules")}>
                  Ràng buộc
                </button>
                <button type="button" className="btn btn-sm" disabled={busy} onClick={() => setDialog("groups")}>
                  Nhóm cột
                </button>
                <button type="button" className="btn btn-sm" disabled={busy} onClick={() => setDialog("totals")}>
                  Dòng tổng
                </button>
              </div>
            ) : (
              <p className="fd-hint">
                Phiên bản {version.status === "proposed" ? "đã đề nghị phát hành (khóa nội dung — rút đề nghị để sửa)" : version.status === "published" ? "đã phát hành — bất biến; tạo phiên bản mới để sửa" : "không sửa được"}.
              </p>
            )}
            <FormGrid
              definition={def}
              rows={[]}
              ariaLabel={`Cấu trúc mẫu ${tpl.code}`}
              showKindRow
              selectedColumn={sel}
              onSelectColumn={(l) => {
                setSel(l === sel ? null : l);
                setFx(0);
              }}
              headerSelection={hsel}
              onHeaderCellClick={(row, col, extend) => {
                const L = def.columns[col - 1]?.letter ?? null;
                if (extend && anchor) setHsel(extendRect(anchor, { row, col }));
                else {
                  setAnchor({ row, col });
                  setHsel({ c1: col, r1: row, c2: col, r2: row });
                  setSel(L);
                }
              }}
              maxHeight={360}
            />
            <p className="fd-hint" aria-live="polite">
              {selCol ? `Đang chọn cột ${selCol.letter} (${KIND_LABEL[selCol.kind]}${selCol.title ? ` · ${selCol.title}` : ""}). ` : "Bấm chữ cái để chọn cột; bấm ô tiêu đề (Shift + bấm để chọn vùng) để gộp/tách ô, sửa chữ. "}
              {hsel ? `Vùng tiêu đề ${rectString(hsel)}${mergeOfSel ? ` (thuộc vùng gộp ${mergeOfSel})` : ""}. ` : ""}
              Dòng cuối là loại cột; màn hình nhập liệu và bảng tổng hợp dựng theo cấu trúc này.
            </p>
            {editable && singleHeader ? <HeaderCellEditor key={`${singleHeader.r1}:${singleHeader.c1}:${headerText}`} row={singleHeader.r1} letter={columnLetter(singleHeader.c1)} text={headerText} busy={busy} onSet={(text) => void ops([{ op: "set_header_cell", row: singleHeader.r1, letter: columnLetter(singleHeader.c1), text }])} /> : null}
            <div className="fd-actions">
              <button type="button" className="btn btn-o" aria-expanded={preview} onClick={() => setPreview((p) => !p)}>
                <Icon name="eye" size={16} /> {preview ? "Ẩn xem trước" : "Xem trước màn hình nhập liệu"}
              </button>
              <VersionActions
                version={version}
                editable={editable}
                busy={busy}
                me={me}
                assigned={assignments.length > 0}
                nameOf={nameOf}
                onWithdraw={() => void run(() => withdrawVersion(adminApi, id, v), "Đã rút đề nghị — phiên bản về nháp.")}
                onPublish={() => void run(() => publishVersion(adminApi, id, v), `Đã phát hành ${tpl.code} v${v}. Từ đây phiên bản bất biến.`)}
                onRetire={() => setRetiring(true)}
                onNewVersion={() =>
                  void newFormVersion(adminApi, id, {})
                    .then((nv) => router.push(`${base}/${id}/v/${nv.version}`))
                    .catch((e: unknown) => setProblem(toApiProblem(e)))
                }
              />
            </div>
          </section>
        </div>
        <div className="fd-side">
          <MetaPanel
            key={`${version.id}:${version.definition_sha256}:${version.effective_from}:${version.status}`}
            def={def}
            version={version}
            tplCode={tpl.code}
            dept={tpl.department_code}
            level={tpl.entry_level}
            reportTypes={assignments.map((a) => REPORT_TYPE_LABEL[a.report_type])}
            editable={editable}
            busy={busy}
            onSave={async (meta, eff) => {
              if (Object.keys(meta).length) {
                const r = await ops([{ op: "set_meta", meta }]);
                if (!r) return;
              }
              if (eff.effective_from || eff.effective_label) {
                await run(() => saveDraft(adminApi, id, v, { effective_from: eff.effective_from || undefined, effective_label: eff.effective_label || undefined }), "Đã lưu nháp.");
              } else setNotice("Đã lưu nháp.");
            }}
            onPropose={(eff) =>
              void run(
                () => proposeVersion(adminApi, id, v, { effective_from: eff.effective_from || undefined, effective_label: eff.effective_label || undefined }),
                "Đã đề nghị phát hành — chờ một Admin khác phát hành (R-56).",
              )
            }
          />
          {selCol ? (
            <ColumnPanel
              key={`${selCol.letter}:${version.definition_sha256}:${fx}`}
              col={selCol}
              startFormula={fx > 0}
              def={def}
              editable={editable}
              busy={busy}
              onApply={(column) => void ops([{ op: "update_column", letter: selCol.letter, column }]).then((r) => r && setFx(0))}
            />
          ) : (
            <section className="card" aria-label="Thuộc tính cột">
              <h2>Thuộc tính cột</h2>
              <p className="fd-hint">Chọn một cột (bấm chữ cái) để sửa tiêu đề, loại cột, kiểu dữ liệu, đơn vị, bắt buộc, danh mục chọn, công thức.</p>
            </section>
          )}
          {def.pending.length ? (
            <section className="card" aria-label="Phần chờ khách">
              <h2>Phần chờ khách ({def.pending.length})</h2>
              <ul className="fd-pend">
                {def.pending.map((p, i) => (
                  <li key={`${p.part}-${i}`}>
                    <b>{p.part}</b>: {p.detail} <span className="mute">({p.question})</span>
                  </li>
                ))}
              </ul>
            </section>
          ) : null}
        </div>
      </div>
      {preview ? <PreviewPanel def={def} version={version} /> : null}
      {dialog === "indicators" ? <IndicatorsDialog def={def} onClose={() => setDialog(null)} onOps={(list) => ops(list)} /> : null}
      {dialog === "rules" ? <RulesDialog def={def} onClose={() => setDialog(null)} onOps={(list) => ops(list)} /> : null}
      {dialog === "groups" ? <GroupsDialog def={def} onClose={() => setDialog(null)} onOps={(list) => ops(list)} /> : null}
      {dialog === "totals" ? <TotalsDialog def={def} onClose={() => setDialog(null)} onOps={(list) => ops(list)} /> : null}
      {dialog === "period" ? (
        <PeriodDialog
          def={def}
          onClose={() => setDialog(null)}
          onChange={async (blockId, slots, pattern) => {
            const ch = periodSlotChange(def, blockId, slots, pattern);
            const r = await ops(ch.ops);
            if (r && ch.fixHeader && r.definition) {
              const fixed = ch.fixHeader(r.definition);
              if (fixed) await run(() => saveDraft(adminApi, id, v, { definition: fixed }));
            }
            return r;
          }}
        />
      ) : null}
      {retiring ? (
        <ReasonDialog
          title={`Ngừng dùng ${tpl.code} v${v}`}
          hint="Công việc đã ghim phiên bản này giữ nguyên; phát hành mới dùng phiên bản khác."
          onClose={() => setRetiring(false)}
          onSubmit={async (reason) => {
            const r = await retireVersion(adminApi, id, v, reason);
            setOverride(r);
            setRetiring(false);
            detail.reload();
          }}
        />
      ) : null}
    </>
  );
}

// ---------------------------------------------------------------------------
// Trạng thái phiên bản: rút đề nghị, phát hành (người khác), ngừng dùng
// ---------------------------------------------------------------------------

function VersionActions(p: {
  version: FormVersion;
  editable: boolean;
  busy: boolean;
  me: string;
  assigned: boolean;
  nameOf: (ref: string | null) => string;
  onWithdraw: () => void;
  onPublish: () => void;
  onRetire: () => void;
  onNewVersion: () => void;
}) {
  const v = p.version;
  if (v.status === "proposed") {
    const self = v.proposed_by === p.me || v.updated_by === p.me;
    return (
      <>
        <button type="button" className="btn btn-o" disabled={p.busy} onClick={p.onWithdraw}>
          Rút đề nghị
        </button>
        <button type="button" className="btn btn-p" disabled={p.busy} onClick={p.onPublish}>
          <Icon name="send" size={16} /> Phát hành phiên bản
        </button>
        <p className="fd-sod" role="note">
          Đề nghị bởi <b>{p.nameOf(v.proposed_by)}</b> lúc {formatDateTime(v.proposed_at)} · sửa cuối bởi <b>{p.nameOf(v.updated_by)}</b>. Người phát hành phải khác người đề nghị và người sửa cuối (R-56)
          {self ? " — bạn là người đề nghị/sửa cuối nên máy chủ sẽ từ chối; cần một Admin khác." : "."}
        </p>
      </>
    );
  }
  if (v.status === "published") {
    return (
      <>
        <button type="button" className="btn btn-o" onClick={p.onRetire}>
          Ngừng dùng phiên bản
        </button>
        <button type="button" className="btn btn-p" onClick={p.onNewVersion}>
          + Tạo phiên bản mới
        </button>
        <span className="fd-hint">
          Phát hành bởi {p.nameOf(v.published_by)} lúc {formatDateTime(v.published_at)} · áp dụng từ {v.effective_label ?? v.effective_from}.
        </span>
      </>
    );
  }
  if (v.status === "retired") return <span className="fd-hint">Đã ngừng dùng: {v.retire_reason}</span>;
  return p.editable && !p.assigned ? <span className="fd-hint">Mẫu chưa gán loại báo cáo — gán ở trang chi tiết mẫu trước khi đề nghị phát hành.</span> : null;
}

// ---------------------------------------------------------------------------
// Thuộc tính mẫu (B4) + Lưu nháp / Đề nghị phát hành (B5)
// ---------------------------------------------------------------------------

function MetaPanel(p: {
  def: FormDefinition;
  version: FormVersion;
  tplCode: string;
  dept: string;
  level: "commune" | "province";
  reportTypes: string[];
  editable: boolean;
  busy: boolean;
  onSave: (meta: NonNullable<FormOp["meta"]>, eff: { effective_from: string; effective_label: string }) => void | Promise<void>;
  onPropose: (eff: { effective_from: string; effective_label: string }) => void;
}) {
  const { def, version } = p;
  const [title, setTitle] = useState(def.title);
  const [sheet, setSheet] = useState(def.sheet_name);
  const [req, setReq] = useState(def.requirements);
  const [freeze, setFreeze] = useState(def.freeze_through);
  const [comp, setComp] = useState(def.completeness);
  const [from, setFrom] = useState(version.effective_from ?? "");
  const [label, setLabel] = useState(version.effective_label ?? "");
  const meta = () => {
    const m: NonNullable<FormOp["meta"]> = {};
    if (title !== def.title) m.title = title;
    if (sheet !== def.sheet_name) m.sheet_name = sheet;
    if (req !== def.requirements) m.requirements = req;
    if (freeze.toUpperCase() !== def.freeze_through) m.freeze_through = freeze.toUpperCase();
    if (comp !== def.completeness) m.completeness = comp;
    return m;
  };
  const effChanged = from !== (version.effective_from ?? "") || label !== (version.effective_label ?? "");
  const ro = !p.editable;
  return (
    <section className="card" aria-labelledby="fd-meta">
      <h2 id="fd-meta">Thuộc tính mẫu</h2>
      <div className="field">
        <label htmlFor="fm-title">Tên mẫu *</label>
        <input id="fm-title" className="input" value={title} disabled={ro} onChange={(e) => setTitle(e.target.value)} />
      </div>
      <div className="fd-two">
        <div className="field">
          <label htmlFor="fm-code">Mã *</label>
          <input id="fm-code" className="input" value={p.tplCode} disabled readOnly />
        </div>
        <div className="field">
          <label htmlFor="fm-ver">Phiên bản</label>
          <input id="fm-ver" className="input" value={`v${version.version} (${version.status === "draft" ? "nháp" : version.status === "proposed" ? "chờ phát hành" : version.status === "published" ? "đã phát hành" : "ngừng dùng"})`} disabled readOnly />
        </div>
      </div>
      <dl className="dl-inline">
        <dt>Phòng ban</dt>
        <dd>{deptName(p.dept)}</dd>
        <dt>Cấp nhập liệu</dt>
        <dd>{LEVEL_LABEL[p.level]}</dd>
        <dt>Loại báo cáo</dt>
        <dd>{p.reportTypes.length ? p.reportTypes.join(", ") : "Chưa gán"}</dd>
      </dl>
      <div className="fd-two">
        <div className="field">
          <label htmlFor="fm-from">Áp dụng từ kỳ (ngày bắt đầu)</label>
          <input id="fm-from" type="date" className="input" value={from} disabled={ro} onChange={(e) => setFrom(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="fm-label">Nhãn kỳ</label>
          <input id="fm-label" className="input" value={label} placeholder="Tuần 37/2026" disabled={ro} onChange={(e) => setLabel(e.target.value)} />
        </div>
      </div>
      <div className="field">
        <label htmlFor="fm-sheet">Tên sheet Excel</label>
        <input id="fm-sheet" className="input" value={sheet} disabled={ro} onChange={(e) => setSheet(e.target.value)} />
      </div>
      <div className="field">
        <label htmlFor="fm-req">Yêu cầu biểu mẫu (nguyên văn)</label>
        <textarea id="fm-req" className="textarea" value={req} disabled={ro} onChange={(e) => setReq(e.target.value)} />
      </div>
      <div className="fd-two">
        <div className="field">
          <label htmlFor="fm-freeze">Cố định tới cột</label>
          <input id="fm-freeze" className="input" value={freeze} disabled={ro} maxLength={3} onChange={(e) => setFreeze(e.target.value)} />
        </div>
        <div className="field">
          <label htmlFor="fm-comp">Mức đầy đủ</label>
          <select id="fm-comp" className="select" value={comp} disabled={ro} onChange={(e) => setComp(e.target.value as FormDefinition["completeness"])}>
            {Object.entries(COMPLETENESS_LABEL).map(([k, l]) => (
              <option key={k} value={k}>
                {l}
              </option>
            ))}
          </select>
        </div>
      </div>
      {p.editable ? (
        <div className="fd-actions">
          <button type="button" className="btn btn-o" disabled={p.busy} onClick={() => void p.onSave(meta(), effChanged ? { effective_from: from, effective_label: label } : { effective_from: "", effective_label: "" })}>
            Lưu nháp
          </button>
          <button type="button" className="btn btn-p" disabled={p.busy} onClick={() => p.onPropose({ effective_from: from, effective_label: label })}>
            <Icon name="send" size={16} /> Đề nghị phát hành
          </button>
        </div>
      ) : null}
      <p className="fd-hint">Cập nhật {formatDateTime(version.updated_at)}. Mỗi thao tác lưới được lưu ngay vào nháp và ghi Log.</p>
    </section>
  );
}

// ---------------------------------------------------------------------------
// Thuộc tính cột (B3)
// ---------------------------------------------------------------------------

const FIXED_SOURCES = [
  { value: "row_no", label: "STT" },
  { value: "region_name", label: "Khu vực" },
  { value: "commune_name", label: "Xã, phường" },
  { value: "indicator_label", label: "Nhãn dòng chỉ tiêu" },
  { value: "none", label: "Trống" },
];

function ColumnPanel({ col, def, editable, busy, onApply, startFormula = false }: { col: FormColumn; def: FormDefinition; editable: boolean; busy: boolean; onApply: (c: FormColumn) => void; startFormula?: boolean }) {
  const [c, setC] = useState<FormColumn>(startFormula && editable ? { ...col, kind: "formula", formula: col.formula ?? "" } : col);
  const set = (patch: Partial<FormColumn>) => setC((x) => ({ ...x, ...patch }));
  const ro = !editable;
  const apply = () => {
    const out: FormColumn = { ...c };
    if (out.kind !== "formula") delete out.formula;
    if (out.kind !== "fixed") delete out.fixed_source;
    if (out.kind !== "per_period" && out.kind !== "formula") {
      delete out.period_block;
      delete out.period_slot;
    }
    if (out.dtype !== "catalog") delete out.catalog;
    for (const k of ["unit", "min", "max", "note", "catalog", "formula"] as const) if (out[k] === "") delete out[k];
    onApply(out);
  };
  return (
    <section className="card" aria-labelledby="fd-col">
      <h2 id="fd-col">Thuộc tính cột {col.letter}</h2>
      <div className="field">
        <label htmlFor="fd-col-title">Tiêu đề</label>
        <input id="fd-col-title" className="input" value={c.title} disabled={ro} onChange={(e) => set({ title: e.target.value })} />
      </div>
      <div className="field">
        <label htmlFor="fd-col-kind">Loại cột</label>
        <select id="fd-col-kind" className="select" value={c.kind} disabled={ro} onChange={(e) => set({ kind: e.target.value as FormColumn["kind"], ...(e.target.value === "fixed" && !c.fixed_source ? { fixed_source: "none" } : {}) })}>
          {Object.entries(KIND_LABEL).map(([k, l]) => (
            <option key={k} value={k}>
              {l}
            </option>
          ))}
        </select>
      </div>
      {c.kind === "formula" ? (
        <div className="field">
          <label htmlFor="fd-col-formula">Công thức (ƒx, tham chiếu cùng dòng: F#, dải F#:L#)</label>
          <input id="fd-col-formula" className="input fd-formula" autoFocus={startFormula} value={c.formula ?? ""} disabled={ro} placeholder="SUM(F#:L#)" onChange={(e) => set({ formula: e.target.value })} />
          <span className="fd-hint">SUM MIN MAX AVERAGE COUNT ROUND ROUNDUP ROUNDDOWN ABS IF IFERROR AND OR NOT ISBLANK ISNUMBER; + − × ÷ ^ &amp; %. Máy chủ kiểm cú pháp, tham chiếu, vòng lặp.</span>
        </div>
      ) : null}
      {c.kind === "fixed" ? (
        <div className="field">
          <label htmlFor="fd-col-src">Nguồn hệ thống</label>
          <select id="fd-col-src" className="select" value={c.fixed_source ?? "none"} disabled={ro} onChange={(e) => set({ fixed_source: e.target.value })}>
            {FIXED_SOURCES.map((s) => (
              <option key={s.value} value={s.value}>
                {s.label}
              </option>
            ))}
            {c.fixed_source?.startsWith("xref:") ? <option value={c.fixed_source}>Liên biểu {c.fixed_source.slice(5)}</option> : null}
          </select>
        </div>
      ) : null}
      {c.kind === "per_period" || (c.kind === "formula" && c.period_block) ? (
        <div className="fd-two">
          <div className="field">
            <label htmlFor="fd-col-block">Khối kỳ</label>
            <select id="fd-col-block" className="select" value={c.period_block ?? ""} disabled={ro} onChange={(e) => set({ period_block: e.target.value || undefined })}>
              <option value="">—</option>
              {def.period_blocks.map((b) => (
                <option key={b.id} value={b.id}>
                  {b.id} ({b.title_pattern})
                </option>
              ))}
            </select>
          </div>
          <div className="field">
            <label htmlFor="fd-col-slot">Kỳ thứ</label>
            <input id="fd-col-slot" type="number" min={1} className="input" value={c.period_slot ?? ""} disabled={ro} onChange={(e) => set({ period_slot: e.target.value ? Number(e.target.value) : undefined })} />
          </div>
        </div>
      ) : null}
      <div className="fd-two">
        <div className="field">
          <label htmlFor="fd-col-dtype">Kiểu dữ liệu</label>
          <select id="fd-col-dtype" className="select" value={c.dtype} disabled={ro} onChange={(e) => set({ dtype: e.target.value as FormColumn["dtype"] })}>
            {Object.entries(DTYPE_LABEL).map(([k, l]) => (
              <option key={k} value={k}>
                {l}
              </option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="fd-col-unit">Đơn vị</label>
          <input id="fd-col-unit" className="input" value={c.unit ?? ""} disabled={ro} onChange={(e) => set({ unit: e.target.value })} />
        </div>
      </div>
      {c.dtype === "catalog" ? (
        <div className="field">
          <label htmlFor="fd-col-cat">Danh mục chọn (mã danh mục)</label>
          <input id="fd-col-cat" className="input" value={c.catalog ?? ""} disabled={ro} placeholder="rice_variety" onChange={(e) => set({ catalog: e.target.value })} />
        </div>
      ) : null}
      {c.dtype === "number" || c.dtype === "integer" ? (
        <div className="fd-two">
          <div className="field">
            <label htmlFor="fd-col-min">Nhỏ nhất</label>
            <input id="fd-col-min" className="input" value={c.min ?? ""} disabled={ro} inputMode="decimal" onChange={(e) => set({ min: e.target.value.replace(",", ".") || undefined })} />
          </div>
          <div className="field">
            <label htmlFor="fd-col-max">Lớn nhất</label>
            <input id="fd-col-max" className="input" value={c.max ?? ""} disabled={ro} inputMode="decimal" onChange={(e) => set({ max: e.target.value.replace(",", ".") || undefined })} />
          </div>
        </div>
      ) : null}
      <label className="row">
        <input type="checkbox" checked={!!c.required} disabled={ro} onChange={(e) => set({ required: e.target.checked || undefined })} /> Bắt buộc
      </label>
      {col.placeholder ? <p className="fd-hint">Cột tạm — chờ khách Q-01 (tệp Excel gốc).</p> : null}
      {editable ? (
        <button type="button" className="btn btn-p btn-sm" disabled={busy} onClick={apply}>
          Áp dụng cho cột {col.letter}
        </button>
      ) : null}
    </section>
  );
}

function HeaderCellEditor({ row, letter, text, busy, onSet }: { row: number; letter: string; text: string; busy: boolean; onSet: (t: string) => void }) {
  const [t, setT] = useState(text);
  return (
    <div className="row" role="group" aria-label="Chữ ô tiêu đề">
      <label htmlFor="fd-hcell" className="small">
        Chữ ô tiêu đề {letter}
        {row}
      </label>
      <input id="fd-hcell" className="input" style={{ minWidth: 280 }} value={t} onChange={(e) => setT(e.target.value)} />
      <button type="button" className="btn btn-o btn-sm" disabled={busy || t === text} onClick={() => onSet(t)}>
        Đặt chữ
      </button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Xem trước màn nhập (B5) — tính trên máy chủ
// ---------------------------------------------------------------------------

function PreviewPanel({ def, version }: { def: FormDefinition; version: FormVersion }) {
  const { adminApi } = useAuth();
  const shown = useMemo(() => withSeason(def, "THU ĐÔNG 2026"), [def]);
  const sample = useMemo(() => sampleData(def, PREVIEW_COMMUNES), [def]);
  const [values, setValues] = useState<{ sha: string; v: GridValues } | null>(null);
  const [slot, setSlot] = useState<string>("");
  const evaluate = useMemo(() => adminEvaluator(adminApi, version.id), [adminApi, version.id]);
  const maxSlot = Math.max(0, ...def.period_blocks.map((b) => b.slots));
  const cur = values && values.sha === version.definition_sha256 ? values.v : sample.values;
  return (
    <section className="card sec" style={{ marginTop: 14 }} aria-labelledby="fd-preview">
      <div className="sec-h">
        <h2 id="fd-preview">Xem trước màn hình nhập liệu — {shown.title}</h2>
        {maxSlot ? (
          <label className="row small">
            Kỳ đang mở
            <select className="select" value={slot} onChange={(e) => setSlot(e.target.value)} aria-label="Kỳ đang mở">
              <option value="">Mọi kỳ (xem trước)</option>
              {Array.from({ length: maxSlot }, (_, i) => (
                <option key={i + 1} value={String(i + 1)}>
                  Kỳ {i + 1}
                </option>
              ))}
            </select>
          </label>
        ) : null}
      </div>
      <p className="fd-hint" style={{ marginBottom: 10 }}>
        Số liệu mẫu (không phải số thật) trên {PREVIEW_COMMUNES.length} xã minh họa; công thức, dòng tổng và kiểm tra do máy chủ tính theo phiên bản v{version.version}.
      </p>
      <FormEntry
        definition={shown}
        versionKey={`${version.id}:${version.definition_sha256}`}
        communes={PREVIEW_COMMUNES}
        values={cur}
        onValuesChange={(next) => setValues({ sha: version.definition_sha256, v: next })}
        evaluate={evaluate}
        system={sample.system}
        openSlots={slot ? [Number(slot)] : null}
        tabStatus={{ [PREVIEW_COMMUNES[0]!.code]: "Lưu nháp", [PREVIEW_COMMUNES[1]!.code]: "Chưa nhập" }}
        maxHeight={420}
      />
    </section>
  );
}

// ---------------------------------------------------------------------------
// Hộp thoại: dòng chỉ tiêu, ràng buộc, khối kỳ, nhóm cột, dòng tổng
// ---------------------------------------------------------------------------

type OpsFn = (ops: FormOp[]) => Promise<FormVersion | null>;

function IndicatorsDialog({ def, onClose, onOps }: { def: FormDefinition; onClose: () => void; onOps: OpsFn }) {
  const [code, setCode] = useState("");
  const [label, setLabel] = useState("");
  const [after, setAfter] = useState("");
  return (
    <Dialog
      title="Dòng chỉ tiêu (mỗi xã × mỗi chỉ tiêu một dòng)"
      onClose={onClose}
      actions={
        <button type="button" className="btn btn-p btn-sm" disabled={!code.trim() || !label.trim()} onClick={() => void onOps([{ op: "add_indicator", indicator: { code: code.trim(), label: label.trim() }, after: after || undefined }]).then((r) => r && (setCode(""), setLabel("")))}>
          + Thêm dòng chỉ tiêu
        </button>
      }
    >
      <ul className="fd-list" style={{ maxHeight: 220, overflow: "auto", marginBottom: 12 }}>
        {def.rows.indicators.map((i, k) => (
          <li key={i.code}>
            <span>
              <b>{i.code}</b> {i.label}
            </span>
            <span className="row" style={{ gap: 4 }}>
              <button type="button" className="btn btn-o btn-sm" aria-label={`Chuyển ${i.code} lên`} disabled={k === 0} onClick={() => void onOps(moveIndicatorOps(def, i.code, -1))}>
                ↑
              </button>
              <button type="button" className="btn btn-o btn-sm" aria-label={`Chuyển ${i.code} xuống`} disabled={k === def.rows.indicators.length - 1} onClick={() => void onOps(moveIndicatorOps(def, i.code, 1))}>
                ↓
              </button>
              <button type="button" className="btn btn-o btn-sm" aria-label={`Xóa dòng chỉ tiêu ${i.code}`} onClick={() => void onOps([{ op: "delete_indicator", code: i.code }])}>
                − Xóa
              </button>
            </span>
          </li>
        ))}
        {!def.rows.indicators.length ? <li className="mute">Mẫu chưa có dòng chỉ tiêu — mỗi xã một dòng.</li> : null}
      </ul>
      <div className="fd-two">
        <div className="field">
          <label htmlFor="fi-ind-code">Mã chỉ tiêu</label>
          <input id="fi-ind-code" className="input" value={code} onChange={(e) => setCode(e.target.value)} placeholder="SB21" />
        </div>
        <div className="field">
          <label htmlFor="fi-ind-after">Chèn sau</label>
          <select id="fi-ind-after" className="select" value={after} onChange={(e) => setAfter(e.target.value)}>
            <option value="">Cuối danh sách</option>
            {def.rows.indicators.map((i) => (
              <option key={i.code} value={i.code}>
                {i.code}
              </option>
            ))}
          </select>
        </div>
      </div>
      <div className="field">
        <label htmlFor="fi-ind-label">Nhãn</label>
        <input id="fi-ind-label" className="input" value={label} onChange={(e) => setLabel(e.target.value)} placeholder="Rầy nâu" />
      </div>
    </Dialog>
  );
}

function RulesDialog({ def, onClose, onOps }: { def: FormDefinition; onClose: () => void; onOps: OpsFn }) {
  const empty: FormRule = { id: "", expr: "", message: "", severity: "error", letters: [] };
  const [r, setR] = useState<FormRule>(empty);
  const [letters, setLetters] = useState("");
  const submit = () => {
    const rule: FormRule = { ...r, id: r.id.trim(), letters: letters.split(/[\s,;]+/).map((x) => x.trim().toUpperCase()).filter(Boolean) };
    if (!rule.when) delete rule.when;
    void onOps([{ op: "set_rule", rule }]).then((x) => x && (setR(empty), setLetters("")));
  };
  return (
    <Dialog
      title="Ràng buộc của mẫu (tính trên từng dòng)"
      onClose={onClose}
      actions={
        <button type="button" className="btn btn-p btn-sm" disabled={!r.id.trim() || !r.expr.trim() || !r.message.trim()} onClick={submit}>
          Lưu ràng buộc
        </button>
      }
    >
      <ul className="fd-list" style={{ maxHeight: 200, overflow: "auto", marginBottom: 12 }}>
        {def.rules.map((x) => (
          <li key={x.id}>
            <span>
              <b>{x.id}</b> <code className="fd-formula">{x.expr}</code> — {x.message} ({x.severity === "error" ? "lỗi" : "cảnh báo"})
            </span>
            <span className="row" style={{ gap: 4 }}>
              <button
                type="button"
                className="btn btn-o btn-sm"
                onClick={() => {
                  setR(x);
                  setLetters(x.letters.join(", "));
                }}
              >
                Sửa
              </button>
              <button type="button" className="btn btn-o btn-sm" aria-label={`Xóa ràng buộc ${x.id}`} onClick={() => void onOps([{ op: "delete_rule", id: x.id }])}>
                Xóa
              </button>
            </span>
          </li>
        ))}
        {!def.rules.length ? <li className="mute">Chưa có ràng buộc.</li> : null}
      </ul>
      <div className="fd-two">
        <div className="field">
          <label htmlFor="fr-id">Mã ràng buộc</label>
          <input id="fr-id" className="input" value={r.id} onChange={(e) => setR({ ...r, id: e.target.value })} placeholder="T05.nang" />
        </div>
        <div className="field">
          <label htmlFor="fr-sev">Mức</label>
          <select id="fr-sev" className="select" value={r.severity} onChange={(e) => setR({ ...r, severity: e.target.value as FormRule["severity"] })}>
            <option value="error">Lỗi (chặn gửi)</option>
            <option value="warning">Cảnh báo</option>
          </select>
        </div>
      </div>
      <div className="field">
        <label htmlFor="fr-expr">Biểu thức (đúng = đạt)</label>
        <input id="fr-expr" className="input fd-formula" value={r.expr} onChange={(e) => setR({ ...r, expr: e.target.value })} placeholder='J#=XREF("T01","M")' />
      </div>
      <div className="field">
        <label htmlFor="fr-when">Chỉ áp dụng khi (tùy chọn)</label>
        <input id="fr-when" className="input fd-formula" value={r.when ?? ""} onChange={(e) => setR({ ...r, when: e.target.value })} placeholder="ISNUMBER(D#)" />
      </div>
      <div className="field">
        <label htmlFor="fr-msg">Thông báo</label>
        <input id="fr-msg" className="input" value={r.message} onChange={(e) => setR({ ...r, message: e.target.value })} />
      </div>
      <div className="field">
        <label htmlFor="fr-letters">Báo tại cột</label>
        <input id="fr-letters" className="input" value={letters} onChange={(e) => setLetters(e.target.value)} placeholder="J, K" />
      </div>
    </Dialog>
  );
}

function PeriodDialog({ def, onClose, onChange }: { def: FormDefinition; onClose: () => void; onChange: (blockId: string, slots: number, pattern: string) => Promise<FormVersion | null> }) {
  const [blockId, setBlockId] = useState(def.period_blocks[0]?.id ?? "");
  const b = def.period_blocks.find((x) => x.id === blockId);
  const [slots, setSlots] = useState(b?.slots ?? 1);
  const [pattern, setPattern] = useState(b?.title_pattern ?? "Tuần {n}");
  const delta = b ? slots - b.slots : 0;
  return (
    <Dialog
      title="Khối cột theo kỳ"
      onClose={onClose}
      actions={
        b ? (
          <button type="button" className="btn btn-p btn-sm" disabled={Math.abs(delta) > 1 || (delta === 0 && pattern === b.title_pattern) || slots < 1} onClick={() => void onChange(b.id, slots, pattern).then((r) => r && onClose())}>
            Áp dụng
          </button>
        ) : null
      }
    >
      {def.period_blocks.length ? (
        <div className="stack">
          <div className="field">
            <label htmlFor="fp-block">Khối</label>
            <select
              id="fp-block"
              className="select"
              value={blockId}
              onChange={(e) => {
                const nb = def.period_blocks.find((x) => x.id === e.target.value);
                setBlockId(e.target.value);
                setSlots(nb?.slots ?? 1);
                setPattern(nb?.title_pattern ?? "");
              }}
            >
              {def.period_blocks.map((x) => (
                <option key={x.id} value={x.id}>
                  {x.id}: từ cột {x.first_letter}, {x.width} cột/kỳ, {x.slots} kỳ
                </option>
              ))}
            </select>
          </div>
          <div className="fd-two">
            <div className="field">
              <label htmlFor="fp-slots">Số kỳ (±1 mỗi lần)</label>
              <input id="fp-slots" type="number" min={1} className="input" value={slots} onChange={(e) => setSlots(Number(e.target.value) || 1)} />
            </div>
            <div className="field">
              <label htmlFor="fp-pattern">Tiêu đề mẫu</label>
              <input id="fp-pattern" className="input" value={pattern} onChange={(e) => setPattern(e.target.value)} />
            </div>
          </div>
          <p className="fd-hint">
            Số cột = số kỳ đã chia (FN-TI-02.02); chỉ cột của kỳ đang mở được nhập. Tăng kỳ: chèn {b?.width ?? 1} cột trước kỳ cuối (dải SUM, vùng gộp nở như Excel). Giảm kỳ: xóa các cột của kỳ cuối — máy chủ từ chối nếu công thức khác còn tham chiếu. Tiêu đề kỳ đặt theo nhãn kỳ khi phát hành; {"{n}"} là số thứ tự kỳ.
          </p>
        </div>
      ) : (
        <p className="mute">Mẫu không có khối cột theo kỳ. Đổi loại cột thành “Cột theo kỳ” cần một khối kỳ (dùng mẫu T01/T07 làm khuôn).</p>
      )}
    </Dialog>
  );
}

function GroupsDialog({ def, onClose, onOps }: { def: FormDefinition; onClose: () => void; onOps: OpsFn }) {
  const [rows, setRows] = useState(def.groups.map((g) => ({ ...g })));
  const set = (i: number, patch: Partial<(typeof rows)[number]>) => setRows((rs) => rs.map((r, k) => (k === i ? { ...r, ...patch } : r)));
  return (
    <Dialog
      title="Nhóm cột (ô “Đi tới” khi cuộn ngang)"
      onClose={onClose}
      actions={
        <button type="button" className="btn btn-p btn-sm" onClick={() => void onOps([{ op: "set_groups", groups: rows.map((r) => ({ ...r, from: r.from.toUpperCase(), to: r.to.toUpperCase() })) }]).then((r) => r && onClose())}>
          Lưu nhóm cột
        </button>
      }
    >
      <table className="tbl">
        <thead>
          <tr>
            <th>Mã</th>
            <th>Tên nhóm</th>
            <th>Từ</th>
            <th>Đến</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i}>
              <td>
                <input className="input" aria-label={`Mã nhóm ${i + 1}`} value={r.id} onChange={(e) => set(i, { id: e.target.value })} style={{ width: 90 }} />
              </td>
              <td>
                <input className="input" aria-label={`Tên nhóm ${i + 1}`} value={r.title} onChange={(e) => set(i, { title: e.target.value })} />
              </td>
              <td>
                <input className="input" aria-label={`Từ cột ${i + 1}`} value={r.from} onChange={(e) => set(i, { from: e.target.value })} style={{ width: 60 }} />
              </td>
              <td>
                <input className="input" aria-label={`Đến cột ${i + 1}`} value={r.to} onChange={(e) => set(i, { to: e.target.value })} style={{ width: 60 }} />
              </td>
              <td>
                <button type="button" className="btn btn-o btn-sm" onClick={() => setRows((rs) => rs.filter((_, k) => k !== i))}>
                  Xóa
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      <button type="button" className="btn btn-o btn-sm" style={{ marginTop: 8 }} onClick={() => setRows((rs) => [...rs, { id: `nhom_${rs.length + 1}`, title: "", from: "", to: "" }])}>
        + Thêm nhóm
      </button>
    </Dialog>
  );
}

function TotalsDialog({ def, onClose, onOps }: { def: FormDefinition; onClose: () => void; onOps: OpsFn }) {
  const find = (k: FormTotalDef["kind"]) => def.totals.find((t) => t.kind === k);
  const [region, setRegion] = useState({ on: !!find("region"), label: find("region")?.label ?? "Cộng {region}", letter: find("region")?.label_letter ?? "B" });
  const [prov, setProv] = useState({ on: !!find("province"), label: find("province")?.label ?? "Toàn tỉnh", letter: find("province")?.label_letter ?? "B" });
  const totals: FormTotalDef[] = [
    ...(region.on ? [{ kind: "region" as const, label: region.label, label_letter: region.letter.toUpperCase() }] : []),
    ...(prov.on ? [{ kind: "province" as const, label: prov.label, label_letter: prov.letter.toUpperCase() }] : []),
  ];
  return (
    <Dialog
      title="Dòng tổng (Khu vực, Toàn tỉnh)"
      onClose={onClose}
      actions={
        <button type="button" className="btn btn-p btn-sm" onClick={() => void onOps([{ op: "set_totals", totals }]).then((r) => r && onClose())}>
          Lưu dòng tổng
        </button>
      }
    >
      {[
        { name: "Cộng khu vực", st: region, set: setRegion },
        { name: "Toàn tỉnh", st: prov, set: setProv },
      ].map((x) => (
        <fieldset key={x.name} className="plain" style={{ marginBottom: 10 }}>
          <legend>
            <label className="row">
              <input type="checkbox" checked={x.st.on} onChange={(e) => x.set({ ...x.st, on: e.target.checked })} /> {x.name}
            </label>
          </legend>
          <div className="fd-two">
            <input className="input" aria-label={`Nhãn ${x.name}`} value={x.st.label} onChange={(e) => x.set({ ...x.st, label: e.target.value })} />
            <input className="input" aria-label={`Cột nhãn ${x.name}`} value={x.st.letter} onChange={(e) => x.set({ ...x.st, letter: e.target.value })} />
          </div>
        </fieldset>
      ))}
      <p className="fd-hint">Cột số cộng các dòng thành viên; cột công thức tính lại trên dòng tổng (năng suất bình quân gia quyền). {"{region}"} = tên khu vực.</p>
    </Dialog>
  );
}
