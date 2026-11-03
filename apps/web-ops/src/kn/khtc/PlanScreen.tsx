"use client";

// FN-TI-02.02 — Kế hoạch năm các chương trình (BRD KHTC v2.2; W11.61, pf/extension). Chọn năm; thanh 6 tab; cây CT/DA →
// nhóm → hạng mục; Phòng KHTC (và Admin) sửa trên bản nháp rồi "Lưu" → phiên bản 1 (ngày hiệu lực mặc định 01/01) hoặc
// "Điều chỉnh theo QĐ" (số, ngày QĐ, ngày hiệu lực bắt buộc) → phiên bản mới; phiên bản cũ xem lại được. "Sao chép từ
// năm trước", "Xuất Excel", "Nhập từ Excel" (thành bản nháp). Danh sách CT/DA chủ (D20) thêm/ngừng ngay trên màn.
// Phòng chuyên môn, lãnh đạo chỉ xem.
import { useEffect, useLayoutEffect, useMemo, useRef, useState, type ChangeEvent } from "react";
import { PLAN_BASE, parsePlanRoute, planHref, type PlanRoute } from "./plans/routes";
import { newVersionHead } from "./plans/version-form";
import { SaveVersionPage, VersionDetailPage, VersionsPage, planVersionLog } from "./plans/VersionPages";
import { EntryPage } from "./plans/EntryPages";
import { PlanTree } from "./plans/PlanTree";
import { entryDraft, type EntryDraft } from "./plans/entry-form";
import { ProgramPage, type ProgramDraft } from "./plans/ProgramPages";
import { entryHref, newEntryHref, programHref } from "./plans/routes";
import { ProblemAlert } from "@/components/ProblemAlert";
import { PageHeader, StatusPill } from "@/components/patterns/list";
import { LoadState, ReadOnlyNotice, useAction, useLoad } from "@/kn/cycle/common";
import { readUpload, saveBlob } from "@/lib/files";
import {
  TAB_KIND,
  blankEntry,
  childrenOf,
  dmy,
  removeSubtree,
  toSave,
  useKhtcApi,
  useKhtcRights,
  type KhtcCatalogItem,
  type KhtcPlan,
  type KhtcPlanEntry,
  type KhtcProgram,
  type KhtcTab,
} from "./api";

function vnYear(): number {
  return new Date(Date.now() + 7 * 3_600_000).getUTCFullYear();
}

interface Loaded {
  year: number;
  plan: KhtcPlan | null;
  programs: KhtcProgram[];
  catalog: KhtcCatalogItem[];
}

export function KhtcPlanScreen({ route: requestedRoute }: { route?: PlanRoute } = {}) {
  const api = useKhtcApi();
  const rights = useKhtcRights();
  const [localRoute, setLocalRoute] = useState<Omit<PlanRoute, "go">>(() => ({ base: PLAN_BASE, year: vnYear(), view: "list" }));
  const route: PlanRoute = requestedRoute ?? { ...localRoute, go: href => {
    const url = new URL(href, "https://local.invalid");
    const next = parsePlanRoute(url.pathname, url.searchParams, vnYear());
    if (next) setLocalRoute(next);
  } };
  const year = route.year;
  const setYear = (y: number) => route.go(planHref(route.base, y));
  const s = useLoad<Loaded>(
    async () => {
      const [plan, programs, catalog] = await Promise.all([api.plan(year), api.programs(true), api.catalog()]);
      return { year, plan, programs: programs.items, catalog: catalog.items };
    },
    [api, year],
  );
  return (
    <div>
      {(!route || route.view === "list") ? <><PageHeader title="Kế hoạch năm các chương trình">
        Lập, điều chỉnh kế hoạch năm theo quyết định; gán phòng phụ trách cho từng hạng mục. Phiên bản cũ giữ nguyên, xem lại được.
      </PageHeader>
      <div className="row" style={{ marginBottom: 12 }}>
        <button type="button" className="btn btn-o btn-sm" onClick={() => setYear(year - 1)} disabled={year <= 2000} aria-label="Năm trước">
          ‹
        </button>
        <strong aria-live="polite">Năm {year}</strong>
        <button type="button" className="btn btn-o btn-sm" onClick={() => setYear(year + 1)} disabled={year >= 2100} aria-label="Năm sau">
          ›
        </button>
      </div>
      {!rights.plan ? <ReadOnlyNotice>Chỉ xem — Phòng Kế hoạch – Tài chính lập và điều chỉnh kế hoạch năm.</ReadOnlyNotice> : null}
      </> : null}
      <LoadState s={s}>
        {(d) => d.year === year ? (
          <Editor
            key={year}
            route={route}
            year={year}
            data={d}
            canEdit={rights.plan}
            reload={s.reload}
          />
        ) : <p role="status">Đang tải kế hoạch năm {year}…</p>}
      </LoadState>
    </div>
  );
}


function Editor({
  year,
  route,
  data,
  canEdit,
  reload,
}: {
  year: number;
  route: PlanRoute;
  data: Loaded;
  canEdit: boolean;
  reload: () => void;
}) {
  const api = useKhtcApi();
  const [entries, setEntries] = useState<KhtcPlanEntry[]>(data.plan?.entries ?? []);
  const [copiedFrom, setCopiedFrom] = useState<number | undefined>(undefined);
  const [dirty, setDirty] = useState(false);
  const [tab, setTab] = useState<KhtcTab>("programs");
  const [drafts,setDrafts]=useState<Record<string,EntryDraft>>({});
  const [programDraft,setProgramDraft]=useState<ProgramDraft>({code:"",name:"",kind:TAB_KIND[route.tab??"programs"]??"program"});
  const programAct=useAction();
  const programSaving=useRef(false);
  const [problems, setProblems] = useState<{ row: number | null; field: string; reason: string }[]>([]);
  const [programs, setPrograms] = useState(data.programs);
  const act = useAction();
  const [plan,setPlan]=useState(data.plan);

  const [saveHead, setSaveHead] = useState(() => newVersionHead(year, !!plan));
  const epoch = useRef({ active: true });
  const saving = useRef(false);
  const revision=useRef(0);
  const routeToken=JSON.stringify([route.view,route.ref,route.parent,route.kind,route.tab,route.version]);
  const currentRoute=useRef({token:routeToken});
  // New epoch on every committed route change, including leave-and-return ABA.
  useLayoutEffect(()=>{currentRoute.current={token:routeToken};},[routeToken]);
  useEffect(() => {
    const scope = { active: true }; epoch.current = scope;
    return () => { scope.active = false; };
  }, [canEdit]);


  const change = (next: KhtcPlanEntry[]) => {
    if (!canEdit) return;
    revision.current++;
    setEntries(next);
    setDirty(true);
  };
  const upsert = (e: KhtcPlanEntry) => change(entries.some((x) => x.ref === e.ref) ? entries.map((x) => (x.ref === e.ref ? e : x)) : [...entries, e]);
  const addChild = (parent:KhtcPlanEntry,kind:"group"|"item") => route.go(newEntryHref(route.base,year,parent.tab,parent.ref,kind));
  const draftKey=JSON.stringify([route.view,route.ref,route.parent,route.kind,route.tab]);
  const parent=entries.find(e=>e.ref===route.parent);
  const selected=entries.find(e=>e.ref===route.ref);
  const freshEntry=useMemo(()=>route.view==="new-entry"&&parent&&parent.kind!=="item"&&(route.kind!=="group"||parent.kind==="program")&&parent.tab===route.tab
    ? blankEntry(parent.tab,route.kind!,parent,Math.min(100000,(childrenOf(entries,parent.ref).length+1)*10)) : null,
    // A route draft keeps its ref even if another asynchronous ACK updates the head.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [draftKey]);
  const currentDraft=(selected||freshEntry)?(drafts[draftKey]??entryDraft((selected||freshEntry)!)):null;
  const closeEntry=()=>{setDrafts(old=>{const next={...old};delete next[draftKey];return next;});route.go(planHref(route.base,year));};


  const copyPrev = async () => {
    const d = await act.run(() => api.copyDraft(year), (r) => `Đã chép ${r.entries.length} dòng còn theo dõi của năm ${r.from_year} (bản nháp — chưa lưu)`);
    if (d) {
      setCopiedFrom(year - 1);
      change(d.entries);
    }
  };
  const exportXlsx = async () => {
    const f = await act.run(() => api.exportXlsx(year, plan?.version.version));
    if (f) saveBlob(f.blob, f.name);
  };
  const importXlsx = async (ev: ChangeEvent<HTMLInputElement>) => {
    const file = ev.target.files?.[0];
    ev.target.value = "";
    if (!file) return;
    const up = await readUpload(file);
    const d = await act.run(() => api.importDraft(year, up), (r) => `Đã đọc ${r.entries.length} dòng từ Excel (bản nháp — chưa lưu)`);
    if (d) {
      setProblems(d.problems);
      change(d.entries);
    }
  };

  if (["entry","edit-entry","new-entry"].includes(route.view)) return currentDraft ? <EntryPage key={draftKey} route={route} draft={currentDraft} onChange={draft=>setDrafts(old=>({...old,[draftKey]:draft}))} canEdit={canEdit} catalog={data.catalog} log={plan?planVersionLog([plan.version]):[]} onSave={entry=>{if(!canEdit)return;upsert(entry);closeEntry();}} onCancel={closeEntry}/> : <p role="alert">Không tìm thấy dòng hoặc cấp cha hợp lệ trong cây của năm này.</p>;
  if (route.view==="programs"||route.view==="new-program") return <ProgramPage key={route.view} route={route} programs={programs} used={new Set(entries.filter(e=>e.tab===(route.tab??"programs")&&e.kind==="program").map(e=>e.program_ref??""))} draft={programDraft} onChange={setProgramDraft} canEdit={canEdit} busy={programAct.busy} problem={programAct.problem} log={plan?planVersionLog([plan.version]):[]} onPick={p=>{
    if(!canEdit||!p.active||entries.some(e=>e.tab===route.tab&&e.program_ref===p.ref&&e.kind==="program"))return;
    const tab=route.tab??"programs";const e=blankEntry(tab,"program",null,Math.min(100000,(childrenOf(entries.filter(e=>e.tab===tab),null).length+1)*10));
    change([...entries,{...e,program_ref:p.ref,program_code:p.code,program_name:p.name,name:p.name}]);setTab(tab);route.go(planHref(route.base,year));
  }} onCreate={async()=>{
    if(!canEdit||programSaving.current)return;programSaving.current=true;const scope=epoch.current;const token=currentRoute.current;
    try { const p=await programAct.run(()=>api.createProgram({...programDraft,code:programDraft.code.trim(),name:programDraft.name.trim()}));
      if(p&&scope.active){setPrograms(old=>old.some(x=>x.ref===p.ref)?old:[...old,p]);setProgramDraft({code:"",name:"",kind:p.kind});if(currentRoute.current===token)route.go(programHref(route.base,year,route.tab??"programs"));}
    }finally{programSaving.current=false;}
  }}/>;
  if (route?.view === "history") return <VersionsPage route={route} />;
  if (route?.view === "version") return <VersionDetailPage route={route} />;
  if (route?.view === "save") return <>
    {act.view}
    <SaveVersionPage route={route} head={saveHead} onChange={setSaveHead} adjust={!!plan} canEdit={canEdit} dirty={dirty} busy={act.busy} problem={act.problem} log={plan ? planVersionLog([plan.version]) : []}
      onCancel={() => { setSaveHead(newVersionHead(year, !!plan)); act.resetIdempotency(); route.go(planHref(route.base, year)); }}
      onSave={async head => {
        if (!canEdit || !dirty || saving.current) return;
        saving.current = true;
        const scope = epoch.current;
        const submittedRevision=revision.current;const submittedRoute=currentRoute.current;
        try {
          const saved = await act.run(() => api.save(year, toSave(entries, { base_version: plan?.latest_version ?? 0, copied_from_year: copiedFrom, ...head })), p => `Đã lưu phiên bản ${p.version.version}`);
          if (saved && scope.active) {
            // Only our confirmed result may advance the CAS base. A changed route suppresses navigation,
            // but must not strand the still-live workspace on the old base or discard later local edits.
            setPlan(saved);setProblems([]);
            if(revision.current===submittedRevision){setEntries(saved.entries);setDirty(false);}
            if(currentRoute.current===submittedRoute){setSaveHead(newVersionHead(year,true));route.go(planHref(route.base,year,"version",saved.version.version));}
            reload();
          }
        } finally { saving.current = false; }
      }} />
  </>;

  return (
    <>
      <div className="row" style={{ marginBottom: 8, flexWrap: "wrap", gap: 8 }}>
        {plan ? (
          <>
            <StatusPill tone="info">
              Phiên bản {plan.version.version}/{plan.latest_version}
            </StatusPill>
            <span className="small">
              {plan.version.decision_no ? `QĐ ${plan.version.decision_no} ngày ${dmy(plan.version.decision_date)} · ` : ""}hiệu lực từ{" "}
              {dmy(plan.version.effective_from)}
            </span>
            <button type="button" className="btn btn-o btn-sm" onClick={() => route.go(planHref(route.base, year, "history"))}>
              Lịch sử phiên bản
            </button>
            <button type="button" className="btn btn-o btn-sm" disabled={act.busy} onClick={exportXlsx}>
              Xuất Excel
            </button>
          </>
        ) : (
          <StatusPill>Năm {year} chưa có kế hoạch</StatusPill>
        )}
        {canEdit ? (
          <>
            {!plan ? (
              <button type="button" className="btn btn-o btn-sm" disabled={act.busy} onClick={copyPrev}>
                Sao chép từ năm trước
              </button>
            ) : null}
            <label className="btn btn-o btn-sm">
              Nhập từ Excel
              <input type="file" accept=".xlsx,.csv" className="sr-only" onChange={importXlsx} aria-label="Chọn tệp Excel kế hoạch năm" />
            </label>
            <button type="button" className="btn btn-p btn-sm" disabled={!dirty || act.busy} onClick={() => route.go(planHref(route.base, year, "save"))}>
              {plan ? "Điều chỉnh theo QĐ" : "Lưu kế hoạch"}
            </button>
            {dirty ? <span className="small mute">Có thay đổi chưa lưu</span> : null}
          </>
        ) : null}
      </div>
      {act.problem ? <ProblemAlert problem={act.problem} /> : null}
      {act.note ? (
        <div className="alert alert-info" role="status">
          {act.note}
        </div>
      ) : null}
      {problems.length ? (
        <div className="alert alert-info" role="note">
          <strong>Lỗi trong tệp nhập:</strong>
          <ul>
            {problems.slice(0, 20).map((p, i) => (
              <li key={i}>
                {p.row ? `Dòng ${p.row}: ` : ""}
                {p.field} — {p.reason}
              </li>
            ))}
          </ul>
        </div>
      ) : null}
      {canEdit ? <button type="button" className="btn btn-o" onClick={()=>route.go(programHref(route.base,year,tab))}>Thêm CT/DA vào tab</button> : null}
      <PlanTree entries={entries} initialTab={tab} onTabChange={setTab} actions={e=><div className="row">
        <button type="button" className="btn btn-o btn-sm" onClick={()=>route.go(entryHref(route.base,year,e.ref))}>Xem</button>
        {canEdit ? <><button type="button" className="btn btn-o btn-sm" onClick={()=>route.go(entryHref(route.base,year,e.ref,true))}>Sửa</button>
          {e.kind==="program"?<button type="button" className="btn btn-o btn-sm" onClick={()=>addChild(e,"group")}>+ Nhóm</button>:null}
          {e.kind!=="item"?<button type="button" className="btn btn-o btn-sm" onClick={()=>addChild(e,"item")}>+ Hạng mục</button>:null}
          <button type="button" className="btn btn-o btn-sm" onClick={()=>change(removeSubtree(entries,e.ref))}>Xóa</button></>:null}
      </div>}/>

    </>
  );
}
