"use client";

// FN-AD-04.06 B1 — Quản lý mẫu báo cáo (Hình 27, phần trên + bảng "Gán mẫu vào loại báo cáo"): lọc Cấp, Loại
// báo cáo, Trạng thái; Tạo mẫu mới (trống / nhân bản), Nạp từ Excel, Khởi tạo 14 mẫu Phòng Trồng trọt.
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMemo, useState, type FormEvent } from "react";
import type { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { Dialog } from "@/components/Dialog";
import { Icon } from "@/components/Icon";
import { FilterBar, FilterChip, PageHeader, RowActions, StatusPill } from "@/components/patterns/list";
import { Loading } from "@/components/ui";
import { readUpload } from "@/lib/files";
import { toApiProblem, useAsync } from "@/lib/useAsync";
import { createForm, importForm, listAssignments, listForms, seedForms } from "../api";
import { COMPLETENESS_LABEL, LEVEL_LABEL, REPORT_TYPE_LABEL, type FormImportReport, type FormReportType, type FormSummary, type FormTemplate, type FormVersion } from "../types";
import { deptName, FormProblem, VersionPill } from "../ui";

export function formsBase(dept: string): string {
  return `/kn/phong-ban/${dept}/mau-bao-cao`;
}

const LEVEL_OPTS = [
  { value: "", label: "Tất cả" },
  { value: "commune", label: "Cấp Xã" },
  { value: "province", label: "Cấp Tỉnh" },
];
const TYPE_OPTS = [{ value: "", label: "Tất cả" }, ...Object.entries(REPORT_TYPE_LABEL).map(([value, label]) => ({ value, label }))];
const STATUS_OPTS = [
  { value: "active", label: "Đang dùng" },
  { value: "retired", label: "Ngừng dùng" },
  { value: "", label: "Tất cả" },
];

function versionText(v: FormVersion | null): string {
  if (!v) return "—";
  return `v${v.version}${v.effective_label ? ` · từ ${v.effective_label}` : v.effective_from ? ` · từ ${v.effective_from}` : ""}`;
}

export function FormListScreen({ dept }: { dept: string }) {
  const { adminApi } = useAuth();
  const router = useRouter();
  const [level, setLevel] = useState("");
  const [rtype, setRtype] = useState("");
  const [status, setStatus] = useState("active");
  const [dialog, setDialog] = useState<"create" | "import" | "seed" | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const base = formsBase(dept);
  const list = useAsync(
    () => listForms(adminApi, { department: dept, level: (level || undefined) as FormTemplate["entry_level"] | undefined, status: (status || undefined) as FormTemplate["status"] | undefined, report_type: (rtype || undefined) as FormReportType | undefined }),
    [adminApi, dept, level, rtype, status],
  );
  const asg = useAsync(() => listAssignments(adminApi, { department: dept }), [adminApi, dept]);
  const all = useAsync(() => listForms(adminApi, { department: dept }), [adminApi, dept]);
  const items = list.data?.items ?? [];
  const reload = () => {
    list.reload();
    asg.reload();
    all.reload();
  };

  // Bảng gán: gom theo (cấp, loại báo cáo) như Hình 27 ("Xã · Báo cáo tuần · T01 – T07 · v1 · Đang áp dụng").
  const groups = useMemo(() => {
    const byId = new Map((all.data?.items ?? []).map((s) => [s.template.id, s]));
    const m = new Map<string, { level: FormTemplate["entry_level"]; type: FormReportType; forms: FormSummary[] }>();
    for (const a of asg.data?.items ?? []) {
      const s = byId.get(a.template_id);
      if (!s) continue;
      const k = `${a.entry_level}|${a.report_type}`;
      if (!m.has(k)) m.set(k, { level: a.entry_level, type: a.report_type, forms: [] });
      m.get(k)!.forms.push(s);
    }
    return [...m.values()].sort((x, y) => (x.level === y.level ? x.type.localeCompare(y.type) : x.level === "commune" ? -1 : 1));
  }, [all.data, asg.data]);

  return (
    <>
      <PageHeader
        title="Quản lý mẫu báo cáo"
        actions={
          <>
            <button type="button" className="btn btn-o" onClick={() => setDialog("seed")}>
              <Icon name="layers" size={16} /> Khởi tạo 14 mẫu
            </button>
            <button type="button" className="btn btn-o" onClick={() => setDialog("import")}>
              <Icon name="upload" size={16} /> Nạp từ Excel
            </button>
            <button type="button" className="btn btn-p" onClick={() => setDialog("create")}>
              + Tạo mẫu mới
            </button>
          </>
        }
      >
        {deptName(dept)} — mẫu nhập liệu dạng lưới Excel của Cấp Tỉnh, Cấp Xã; phiên bản nháp → đề nghị → phát hành bởi người khác.
      </PageHeader>
      <FilterBar
        onReset={() => {
          setLevel("");
          setRtype("");
          setStatus("active");
        }}
      >
        <FilterChip label="Phòng ban" value={dept} options={[{ value: dept, label: deptName(dept) }]} onChange={() => undefined} defaultValue={dept} />
        <FilterChip label="Cấp" value={level} options={LEVEL_OPTS} onChange={setLevel} />
        <FilterChip label="Loại báo cáo" value={rtype} options={TYPE_OPTS} onChange={setRtype} />
        <FilterChip label="Trạng thái" value={status} options={STATUS_OPTS} onChange={setStatus} defaultValue="active" />
      </FilterBar>
      {notice ? (
        <div className="alert alert-info" role="status" style={{ marginBottom: 12 }}>
          <Icon name="info" />
          <span>{notice}</span>
        </div>
      ) : null}
      {list.error ? <FormProblem problem={list.error} /> : null}
      <div className="card dt-card">
        <div className="tbl-wrap">
          <table className="dt">
            <caption className="sr-only">Danh sách mẫu báo cáo</caption>
            <thead>
              <tr>
                <th scope="col">STT</th>
                <th scope="col">Mã</th>
                <th scope="col">Tên mẫu</th>
                <th scope="col">Cấp</th>
                <th scope="col">Loại báo cáo</th>
                <th scope="col">Phiên bản phát hành</th>
                <th scope="col">Đang soạn</th>
                <th scope="col">Mức đầy đủ</th>
                <th scope="col">Trạng thái</th>
                <th scope="col">
                  <span className="sr-only">Thao tác</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {list.loading && !list.data ? (
                <tr>
                  <td colSpan={10}>
                    <Loading />
                  </td>
                </tr>
              ) : items.length ? (
                items.map((s, i) => (
                  <tr key={s.template.id}>
                    <td className="stt">{i + 1}</td>
                    <td>
                      <b>{s.template.code}</b>
                    </td>
                    <td>
                      <Link href={`${base}/${s.template.id}`}>{s.template.name}</Link>
                    </td>
                    <td>{LEVEL_LABEL[s.template.entry_level]}</td>
                    <td>{s.report_types.length ? s.report_types.map((t) => REPORT_TYPE_LABEL[t]).join(", ") : <span className="mute">Chưa gán</span>}</td>
                    <td>{versionText(s.published_version)}</td>
                    <td>
                      {s.open_version ? (
                        <span className="row" style={{ gap: 6 }}>
                          v{s.open_version.version} <VersionPill status={s.open_version.status} />
                        </span>
                      ) : (
                        "—"
                      )}
                    </td>
                    <td className="small">{COMPLETENESS_LABEL[(s.open_version ?? s.published_version)?.completeness ?? "complete"]}</td>
                    <td>{s.template.status === "active" ? <StatusPill tone="ok">Đang dùng</StatusPill> : <StatusPill tone="bad">Ngừng dùng</StatusPill>}</td>
                    <td>
                      <RowActions
                        label={`mẫu ${s.template.code}`}
                        viewHref={`${base}/${s.template.id}`}
                        editHref={s.open_version ? `${base}/${s.template.id}/v/${s.open_version.version}` : undefined}
                      />
                    </td>
                  </tr>
                ))
              ) : (
                <tr>
                  <td colSpan={10} className="empty">
                    Chưa có mẫu phù hợp bộ lọc. Dùng “Khởi tạo 14 mẫu” để tạo bộ mẫu Phòng Trồng trọt.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </div>

      <h2 className="sec-title" id="gan-mau">
        Gán mẫu vào loại báo cáo của phòng ban
      </h2>
      <div className="card dt-card">
        <table className="dt" aria-labelledby="gan-mau">
          <thead>
            <tr>
              <th scope="col">Phòng ban</th>
              <th scope="col">Cấp</th>
              <th scope="col">Loại báo cáo</th>
              <th scope="col">Mẫu</th>
              <th scope="col">Phiên bản</th>
              <th scope="col">Trạng thái</th>
            </tr>
          </thead>
          <tbody>
            {groups.length ? (
              groups.map((g) => {
                const published = g.forms.filter((f) => f.published_version);
                return (
                  <tr key={`${g.level}-${g.type}`}>
                    <td>{deptName(dept)}</td>
                    <td>{g.level === "commune" ? "Xã" : "Tỉnh"}</td>
                    <td>{REPORT_TYPE_LABEL[g.type]}</td>
                    <td>
                      {g.forms.map((f, i) => (
                        <span key={f.template.id}>
                          {i ? ", " : ""}
                          <Link href={`${base}/${f.template.id}`}>{f.template.code}</Link>
                        </span>
                      ))}
                    </td>
                    <td>{[...new Set(g.forms.map((f) => (f.published_version ? `v${f.published_version.version}` : f.open_version ? `v${f.open_version.version}` : "—")))].join(", ")}</td>
                    <td>{published.length === g.forms.length ? <StatusPill tone="ok">Đang áp dụng</StatusPill> : published.length ? <StatusPill tone="warn">{`${published.length}/${g.forms.length} đã phát hành`}</StatusPill> : <StatusPill>Nháp</StatusPill>}</td>
                  </tr>
                );
              })
            ) : (
              <tr>
                <td colSpan={6} className="empty">
                  Chưa gán mẫu nào. Mẫu chưa gán loại báo cáo không phát hành được.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {dialog === "create" ? (
        <CreateDialog
          dept={dept}
          forms={all.data?.items ?? []}
          onClose={() => setDialog(null)}
          onCreated={(id, v) => {
            setDialog(null);
            router.push(`${base}/${id}/v/${v}`);
          }}
        />
      ) : null}
      {dialog === "import" ? <ImportDialog dept={dept} forms={all.data?.items ?? []} onClose={() => setDialog(null)} onDone={reload} base={base} /> : null}
      {dialog === "seed" ? (
        <SeedDialog
          dept={dept}
          onClose={() => setDialog(null)}
          onDone={(msg) => {
            setDialog(null);
            setNotice(msg);
            reload();
          }}
        />
      ) : null}
    </>
  );
}

function CreateDialog({ dept, forms, onClose, onCreated }: { dept: string; forms: FormSummary[]; onClose: () => void; onCreated: (id: string, v: number) => void }) {
  const { adminApi } = useAuth();
  const [code, setCode] = useState("");
  const [name, setName] = useState("");
  const [level, setLevel] = useState<"commune" | "province">("province");
  const [from, setFrom] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const sources = forms.flatMap((f) => [f.published_version, f.open_version].filter((v): v is FormVersion => !!v).map((v) => ({ id: v.id, label: `${f.template.code} v${v.version} (${f.template.name})` })));
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    try {
      const r = await createForm(adminApi, { code: code.trim().toUpperCase(), name: name.trim(), department_code: dept, entry_level: level, clone_from_version_id: from || undefined });
      onCreated(r.template.id, r.version.version);
    } catch (err) {
      setProblem(toApiProblem(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      title="Tạo mẫu mới"
      onClose={onClose}
      actions={
        <button type="submit" form="fd-create" className="btn btn-p btn-sm" disabled={busy}>
          {busy ? "Đang tạo…" : "Tạo nháp v1"}
        </button>
      }
    >
      <form id="fd-create" className="stack" onSubmit={(e) => void submit(e)}>
        <div className="fd-two">
          <div className="field">
            <label htmlFor="fc-code">Mã mẫu *</label>
            <input id="fc-code" className="input" value={code} onChange={(e) => setCode(e.target.value)} placeholder="vd CC02" required />
          </div>
          <div className="field">
            <label htmlFor="fc-level">Cấp nhập liệu *</label>
            <select id="fc-level" className="select" value={level} onChange={(e) => setLevel(e.target.value as "commune" | "province")}>
              <option value="commune">Cấp Xã</option>
              <option value="province">Cấp Tỉnh</option>
            </select>
          </div>
        </div>
        <div className="field">
          <label htmlFor="fc-name">Tên mẫu *</label>
          <input id="fc-name" className="input" value={name} onChange={(e) => setName(e.target.value)} required />
        </div>
        <div className="field">
          <label htmlFor="fc-from">Khởi tạo từ</label>
          <select id="fc-from" className="select" value={from} onChange={(e) => setFrom(e.target.value)}>
            <option value="">Mẫu trống (STT, Khu vực, Xã + 1 cột nhập)</option>
            {sources.map((s) => (
              <option key={s.id} value={s.id}>
                Nhân bản {s.label}
              </option>
            ))}
          </select>
        </div>
        <p className="fd-hint">Mẫu thuộc {deptName(dept)}; tạo xong là phiên bản nháp v1 — gán loại báo cáo rồi đề nghị phát hành.</p>
        {problem ? <FormProblem problem={problem} /> : null}
      </form>
    </Dialog>
  );
}

function ImportDialog({ dept, forms, onClose, onDone, base }: { dept: string; forms: FormSummary[]; onClose: () => void; onDone: () => void; base: string }) {
  const { adminApi } = useAuth();
  const [mode, setMode] = useState<"new" | "draft">("new");
  const [code, setCode] = useState("");
  const [name, setName] = useState("");
  const [level, setLevel] = useState<"commune" | "province">("commune");
  const [tplId, setTplId] = useState("");
  const [file, setFile] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [result, setResult] = useState<{ report: FormImportReport; href: string; code: string; version: number } | null>(null);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!file) return;
    setBusy(true);
    setProblem(null);
    try {
      const up = await readUpload(file);
      const r = await importForm(adminApi, up, mode === "new" ? { code: code.trim().toUpperCase(), name: name.trim(), department_code: dept, entry_level: level } : { template_id: tplId });
      setResult({ report: r.report, href: `${base}/${r.template.id}/v/${r.version.version}`, code: r.template.code, version: r.version.version });
      onDone();
    } catch (err) {
      setProblem(toApiProblem(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      title="Nạp mẫu từ Excel"
      onClose={onClose}
      actions={
        result ? (
          <Link className="btn btn-p btn-sm" href={result.href}>
            Mở nháp {result.code} v{result.version}
          </Link>
        ) : (
          <button type="submit" form="fd-import" className="btn btn-p btn-sm" disabled={busy || !file}>
            {busy ? "Đang nạp…" : "Nạp tệp"}
          </button>
        )
      }
    >
      {result ? (
        <section aria-label="Kết quả nạp" className="stack">
          <dl className="kv">
            <dt>Sheet</dt>
            <dd>{result.report.sheet}</dd>
            <dt>Dòng tiêu đề</dt>
            <dd>{result.report.header_rows}</dd>
            <dt>Dòng dữ liệu đầu</dt>
            <dd>{result.report.data_row}</dd>
            <dt>Số cột</dt>
            <dd>{result.report.columns}</dd>
            <dt>Vùng gộp</dt>
            <dd>{result.report.merges}</dd>
            <dt>Công thức</dt>
            <dd>{result.report.formulas}</dd>
            <dt>Dòng loại cột</dt>
            <dd>{result.report.kind_row ? "Có (tệp do hệ thống xuất)" : "Không"}</dd>
          </dl>
          {result.report.warnings?.length ? (
            <div className="alert alert-warn" role="status">
              <Icon name="alert" />
              <div>
                <b>Cảnh báo khi nạp ({result.report.warnings.length})</b>
                <ul style={{ margin: "4px 0 0 18px" }}>
                  {result.report.warnings.map((w) => (
                    <li key={w}>{w}</li>
                  ))}
                </ul>
              </div>
            </div>
          ) : (
            <p className="mute">Không có cảnh báo.</p>
          )}
        </section>
      ) : (
        <form id="fd-import" className="stack" onSubmit={(e) => void submit(e)}>
          <fieldset className="plain">
            <legend>Nạp thành</legend>
            <label className="row">
              <input type="radio" name="imp-mode" checked={mode === "new"} onChange={() => setMode("new")} /> Mẫu mới
            </label>
            <label className="row">
              <input type="radio" name="imp-mode" checked={mode === "draft"} onChange={() => setMode("draft")} /> Nháp mới của mẫu có sẵn
            </label>
          </fieldset>
          {mode === "new" ? (
            <>
              <div className="fd-two">
                <div className="field">
                  <label htmlFor="fi-code">Mã mẫu *</label>
                  <input id="fi-code" className="input" value={code} onChange={(e) => setCode(e.target.value)} required />
                </div>
                <div className="field">
                  <label htmlFor="fi-level">Cấp nhập liệu</label>
                  <select id="fi-level" className="select" value={level} onChange={(e) => setLevel(e.target.value as "commune" | "province")}>
                    <option value="commune">Cấp Xã</option>
                    <option value="province">Cấp Tỉnh</option>
                  </select>
                </div>
              </div>
              <div className="field">
                <label htmlFor="fi-name">Tên mẫu *</label>
                <input id="fi-name" className="input" value={name} onChange={(e) => setName(e.target.value)} required />
              </div>
            </>
          ) : (
            <div className="field">
              <label htmlFor="fi-tpl">Mẫu</label>
              <select id="fi-tpl" className="select" value={tplId} onChange={(e) => setTplId(e.target.value)} required>
                <option value="">Chọn mẫu</option>
                {forms
                  .filter((f) => f.template.status === "active")
                  .map((f) => (
                    <option key={f.template.id} value={f.template.id} disabled={!!f.open_version}>
                      {f.template.code} — {f.template.name}
                      {f.open_version ? " (đang có nháp)" : ""}
                    </option>
                  ))}
              </select>
            </div>
          )}
          <div className="field">
            <label htmlFor="fi-file">Tệp Excel (.xlsx) *</label>
            <input id="fi-file" type="file" accept=".xlsx,application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" onChange={(e) => setFile(e.target.files?.[0] ?? null)} />
          </div>
          <p className="fd-hint">Máy chủ đọc khung tiêu đề gộp, hàng chữ cái A, B, C…, dòng loại cột (tệp do hệ thống xuất) và công thức của dòng dữ liệu; công thức tham chiếu dòng/sheet khác thành cột nhập kèm cảnh báo.</p>
          {problem ? <FormProblem problem={problem} /> : null}
        </form>
      )}
    </Dialog>
  );
}

function SeedDialog({ dept, onClose, onDone }: { dept: string; onClose: () => void; onDone: (msg: string) => void }) {
  const { adminApi } = useAuth();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const run = async () => {
    setBusy(true);
    setProblem(null);
    try {
      const r = await seedForms(adminApi, dept);
      onDone(`Khởi tạo xong: tạo ${r.created.length} mẫu nháp${r.created.length ? ` (${r.created.join(", ")})` : ""}; bỏ qua ${r.skipped.length} mẫu đã có${r.skipped.length ? ` (${r.skipped.join(", ")})` : ""}.`);
    } catch (err) {
      setProblem(toApiProblem(err));
    } finally {
      setBusy(false);
    }
  };
  return (
    <Dialog
      title="Khởi tạo 14 mẫu Phòng Trồng trọt"
      onClose={onClose}
      actions={
        <button type="button" className="btn btn-p btn-sm" disabled={busy} onClick={() => void run()}>
          {busy ? "Đang khởi tạo…" : "Khởi tạo"}
        </button>
      }
    >
      <p>
        Tạo T01–T07 (Báo cáo tuần), M01 (Báo cáo tháng), V01–V06 (Báo cáo hàng vụ) cấp Xã thành <b>nháp v1</b> và gán loại báo cáo. Mẫu đã có (cùng mã) giữ nguyên.
      </p>
      <p className="fd-hint" style={{ marginTop: 8 }}>
        Phần chưa biết từ tệp Excel gốc được đánh dấu “chờ khách Q-01”; khi khách gửi tệp, nạp bằng “Nạp từ Excel” thành nháp mới. Phát hành vẫn qua đề nghị → người khác duyệt.
      </p>
      {problem ? <FormProblem problem={problem} /> : null}
    </Dialog>
  );
}
