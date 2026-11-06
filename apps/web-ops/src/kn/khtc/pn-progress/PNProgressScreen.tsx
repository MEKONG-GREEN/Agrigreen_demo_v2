"use client";

import { useEffect, useMemo, useRef, useState, type MutableRefObject } from "react";
import type { ApiClient } from "@/api/client";
import { ApiProblem } from "@/api/problem";
import { useAuth } from "@/auth/AuthProvider";
import { SelectField } from "@/components/form";
import { ProblemAlert } from "@/components/ProblemAlert";
import { DetailPage } from "@/components/patterns/detail";
import { DataTable, StatusPill, type Column } from "@/components/patterns/list";
import { formatDateTime } from "@/lib/format";
import { saveBlob } from "@/lib/files";
import { useInboxAuthorityKey } from "../inbox/inbox-workspace";
import { createPNProgressApi, pnDepartment, PN_DEPARTMENTS, type PNDepartment, type PNLevel, type PNPeriod, type PNProgress, type PNProgressApi, type PNRow } from "./api";

const YEARS = Array.from({ length: 101 }, (_, i) => ({ value: String(2000 + i), label: String(2000 + i) }));
const LEVELS = [
  { value: "achieved", label: "Đạt kế hoạch" }, { value: "progressing", label: "Đang thực hiện" },
  { value: "slow", label: "Chậm tiến độ" }, { value: "qualitative", label: "Định tính" },
];
function asProblem(error: unknown): ApiProblem {
  return error instanceof ApiProblem ? error : new ApiProblem({ type: "about:blank", status: 502, code: "internal_error", title: "Không đọc được dữ liệu tiến độ. Thử lại sau." });
}

export function PNProgressScreen({ department: requested }: { department?: string }) {
  const auth = useAuth();
  const identity = useInboxAuthorityKey();
  const selected = auth.kn.context;
  const department = requested || selected?.department_code || "";
  const allowed = auth.status === "authenticated" && !!auth.principal && auth.kn.status === "active" && !!selected
    && ["admin", "kn_tinh"].includes(selected.role) && pnDepartment(department)
    && (selected.role === "admin" || selected.department_code === department);
  if (!allowed) return <DetailPage title="Tiến độ chương trình của phòng" back={{ href: "/kn/bao-cao-khtc", label: "Báo cáo cho KHTC" }}><p role="alert">Chọn ngữ cảnh phòng chuyên môn phù hợp để xem tiến độ.</p></DetailPage>;
  return <PNBody key={`${identity}:${department}`} api={auth.api} department={department as PNDepartment} backHref={requested ? `/kn/phong-ban/${department}/bao-cao-khtc` : "/kn/bao-cao-khtc"} />;
}

function PNBody({ api, department, backHref }: { api: ApiClient; department: PNDepartment; backHref: string }) {
  const client = useMemo(() => createPNProgressApi(api), [api]);
  const [year, setYear] = useState(() => new Intl.DateTimeFormat("en", { timeZone: "Asia/Ho_Chi_Minh", year: "numeric" }).format(new Date()));
  return <DetailPage title="Tiến độ chương trình của phòng" back={{ href: backHref, label: "Báo cáo cho KHTC" }} badge={<StatusPill tone="neutral">Chỉ xem</StatusPill>}>
    <p>{PN_DEPARTMENTS[department]}. Xem số liệu theo bản Mẫu 5 đã được KHTC chuẩn bị; mốc số liệu và phiên bản hiển thị cùng bảng.</p>
    <div className="row"><SelectField label="Năm kế hoạch" name="year" value={year} onChange={setYear} options={YEARS} required /></div>
    {year ? <PNYear key={year} client={client} department={department} year={year} /> : <p role="status">Chọn năm kế hoạch.</p>}
  </DetailPage>;
}

function PNYear({ client, department, year }: { client: PNProgressApi; department: PNDepartment; year: string }) {
  const [state, setState] = useState<{ items: PNPeriod[]; loading: boolean; next: string | null; problem: ApiProblem | null }>({ items: [], loading: true, next: null, problem: null });
  const [period, setPeriod] = useState("");
  const [reload, setReload] = useState(0);
  const alive = useRef(true);
  useEffect(() => { alive.current = true; return () => { alive.current = false; }; }, []);
  useEffect(() => {
    let active = true;
    void client.periods(department, year).then(page => {
      if (!active) return;
      setState({ items: page.items, loading: false, next: page.has_more ? page.next_after : null, problem: null });
      setPeriod(page.items[0]?.id || "");
    }, error => { if (active) setState({ items: [], loading: false, next: null, problem: asProblem(error) }); });
    return () => { active = false; };
  }, [client, department, year, reload]);
  async function more() {
    if (!state.next || state.loading) return;
    const after = state.next;
    setState(s => ({ ...s, loading: true, problem: null }));
    try {
      const page = await client.periods(department, year, after);
      if (!alive.current) return;
      setState(s => ({ items: [...s.items, ...page.items.filter(p => !s.items.some(old => old.id === p.id))], loading: false, next: page.has_more ? page.next_after : null, problem: null }));
    } catch (error) { if (alive.current) setState(s => ({ ...s, loading: false, problem: asProblem(error) })); }
  }
  if (state.problem) return <ProblemAlert problem={state.problem} onRetry={() => setReload(n => n + 1)} />;
  if (state.loading && !state.items.length) return <p role="status">Đang đọc các kỳ được giao cho phòng…</p>;
  if (!state.items.length) return <p role="status">Chưa có kỳ tiến độ dành cho phòng trong năm này.</p>;
  return <>
    <div className="row">
      <SelectField label="Kỳ báo cáo" name="period" value={period} onChange={setPeriod} options={state.items.map(p => ({ value: p.id, label: p.name }))} required />
      {state.next && <button type="button" className="btn" onClick={() => void more()} disabled={state.loading}>{state.loading ? "Đang đọc…" : "Xem thêm kỳ"}</button>}
    </div>
    {period ? <PNPeriodView key={period} client={client} department={department} period={period} /> : <p role="status">Chọn kỳ để xem tiến độ.</p>}
  </>;
}

function PNPeriodView({ client, department, period }: { client: PNProgressApi; department: PNDepartment; period: string }) {
  const [program, setProgram] = useState("");
  const [level, setLevel] = useState<PNLevel>("");
  const [programs, setPrograms] = useState<PNProgress["programs"]>([]);
  const [nonce, setNonce] = useState(0);
  const snapshot = useRef<string | undefined>(undefined);
  return <>
    <div className="row">
      <SelectField label="Chương trình" name="program" value={program} onChange={setProgram} placeholder="Tất cả chương trình" options={programs.map(p => ({ value: p.ref, label: `${p.roman ? p.roman + ". " : ""}${p.name}` }))} />
      <SelectField label="Mức đạt" name="level" value={level} onChange={v => setLevel(v as PNLevel)} placeholder="Tất cả mức đạt" options={LEVELS} />
      <button type="button" className="btn" onClick={() => { snapshot.current = undefined; setNonce(n => n + 1); }}>Đọc bản Mẫu 5 mới</button>
    </div>
    <PNSnapshotView key={`${program}:${level}:${nonce}`} client={client} department={department} period={period} program={program} level={level} snapshot={snapshot} setPrograms={setPrograms} />
  </>;
}

function PNSnapshotView({ client, department, period, program, level, snapshot, setPrograms }: {
  client: PNProgressApi; department: PNDepartment; period: string; program: string; level: PNLevel;
  snapshot: MutableRefObject<string | undefined>; setPrograms: (p: PNProgress["programs"]) => void;
}) {
  const [data, setData] = useState<PNProgress | null>(null);
  const [problem, setProblem] = useState<ApiProblem | null>(null);
  const [busy, setBusy] = useState(false);
  const [page, setPage] = useState(1);
  const [retry, setRetry] = useState(0);
  const alive = useRef(true);
  useEffect(() => {
    let active = true;
    alive.current = true;
    void client.get(department, period, program, level, snapshot.current).then(out => {
      if (!active) return;
      if (out.snapshot) snapshot.current = out.snapshot.id;
      setPrograms(out.programs); setData(out); setProblem(null);
    }, error => { if (active) { setData(null); setProblem(asProblem(error)); } });
    return () => { active = false; alive.current = false; };
  }, [client, department, period, program, level, snapshot, setPrograms, retry]);
  async function download() {
    if (!data?.snapshot || busy) return;
    setBusy(true); setProblem(null);
    try {
      const file = await client.excel(department, period, data.snapshot.id, program, level);
      if (alive.current) saveBlob(file.blob, file.name);
    } catch (error) { if (alive.current) setProblem(asProblem(error)); }
    finally { if (alive.current) setBusy(false); }
  }
  if (!data) return problem ? <ProblemAlert problem={problem} onRetry={() => setRetry(n => n + 1)} /> : <p role="status">Đang đọc bản Mẫu 5 của phòng…</p>;
  if (data.status === "not_prepared" || !data.snapshot) return <p role="status">KHTC chưa chuẩn bị bản Mẫu 5 cho kỳ này. Chưa có số liệu để xem hoặc xuất Excel.</p>;
  const columns: Column<PNRow>[] = [
    { key: "name", header: "Chương trình / hạng mục", cell: r => r.kind === "item" ? r.name : <strong>{r.roman ? `${r.roman}. ` : ""}{r.name}</strong> },
    { key: "unit", header: "ĐVT", cell: r => r.unit },
    { key: "planned", header: "KH năm", cell: r => r.kind === "item" ? r.planned ?? "—" : "" },
    { key: "quantity", header: "Lũy kế", cell: r => r.kind === "item" ? r.year_to_date : "" },
    { key: "percent", header: "% KH", cell: r => r.kind === "item" ? r.missing || r.percent === null ? "—" : r.percent : "" },
  ];
  return <section aria-label="Tiến độ từ Mẫu 5">
    <div className="row"><StatusPill tone="info">Mẫu 5 v{data.snapshot.version}</StatusPill><span>Mốc số liệu: {formatDateTime(data.snapshot.captured_at)}</span><button type="button" className="btn btn-p" disabled={busy} onClick={() => void download()}>{busy ? "Đang tải…" : "Xuất Excel"}</button></div>
    {problem && <ProblemAlert problem={problem} />}
    {data.rows.some(r => r.missing) && <p role="status">Một số hạng mục còn thiếu nguồn đã chốt hoặc phiếu đã gửi; phần trăm chưa đủ dữ liệu hiển thị “—”.</p>}
    <DataTable caption="Tiến độ chương trình của phòng" columns={columns} rows={data.rows.slice((page - 1) * 20, page * 20)} rowKey={r => r.ref} page={page} pageSize={20} total={data.rows.length} onPage={setPage} empty="Chưa có hạng mục phù hợp bộ lọc của phòng." />
  </section>;
}
