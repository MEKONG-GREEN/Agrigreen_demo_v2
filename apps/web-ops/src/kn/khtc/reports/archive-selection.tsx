"use client";
import { createContext, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useAuth } from "@/auth/AuthProvider";
import { useAction } from "@/kn/cycle/common";
import { saveBlob } from "@/lib/files";
import { createArchiveApi, type ArchiveFile, type ArchivePeriod, type ArchiveQuery } from "./archive-api";
export type SelectedArchiveFile = ArchiveFile & {label: string};
export const archiveFileKey = (file: ArchiveFile) => file.format === "word" ? `word:${file.run_id}` : `excel:${file.export_id}`;
const Selection = createContext<ReturnType<typeof useSelectionState> | null>(null);
function useSelectionState() {
  const [files, setFiles] = useState<SelectedArchiveFile[]>([]);
  const [filter, setFilter] = useState<ArchiveQuery>(() => ({year: new Date(Date.now() + 7*3_600_000).getUTCFullYear(), offset: 0, limit: 20}));
  const [period, setPeriod] = useState<ArchivePeriod | null>(null);
  const toggle = (file: SelectedArchiveFile, checked: boolean) => setFiles(old => checked ? old.some(x=>archiveFileKey(x)===archiveFileKey(file)) || old.length >= 10 ? old : [...old,file] : old.filter(x=>archiveFileKey(x)!==archiveFileKey(file)));
  return {files, setFiles, toggle, filter, setFilter, period, setPeriod};
}
function Provider({children}: {children: ReactNode}) { const value = useSelectionState(); return <Selection.Provider value={value}>{children}</Selection.Provider>; }
export function ArchiveSelectionBoundary({children}: {children: ReactNode}) {
  const outer = useContext(Selection), auth = useAuth();
  const scope = JSON.stringify([auth.status, auth.principal, auth.kn?.context, auth.kn?.support]);
  return outer ? children : <Provider key={scope}>{children}</Provider>;
}
export function useArchiveSelection() { const value=useContext(Selection); if (!value) throw new Error("Archive selection boundary missing"); return value; }
export function useArchiveAlive() { const alive=useRef(true); useEffect(()=>{alive.current=true; return()=>{alive.current=false;};},[]); return alive; }
export function ArchiveBatchBar() {
  const {files,setFiles} = useArchiveSelection(), auth = useAuth();
  const api = useMemo(()=>auth.api ? createArchiveApi(auth.api): null,[auth.api]);
  const action=useAction(), alive=useArchiveAlive(), mixed=files.some(x=>x.format==="word");
  return <section aria-label="Tệp đã chọn" className="card sec">
    <div className="row"><span>Đã chọn {files.length}/10 {mixed ? "tệp Word/Excel" : "bản Excel"} · tối đa 20 MiB</span>
    <button type="button" className="btn" disabled={!api || !files.length || action.busy} onClick={()=>{if(api)void action.run(async()=>{const file=await api.zip(files.map(({label: _label,...target})=>{void _label;return target;}));if(alive.current)saveBlob(file.blob,file.name);return true;},"Đã nhận ZIP các tệp đã lưu.");}}>Tải ZIP {mixed ? "Word/Excel" : "Excel"}</button>
    <button type="button" className="btn btn-o" disabled={!files.length} onClick={()=>setFiles([])}>Bỏ chọn</button></div>
    {files.length > 0 && <ul>{files.map(file=><li key={archiveFileKey(file)}>{file.label}</li>)}</ul>}{action.view}
  </section>;
}
