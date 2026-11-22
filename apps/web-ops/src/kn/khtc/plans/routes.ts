import { TABS, type KhtcTab } from "../api";
export const PLAN_BASE = "/kn/khtc/ke-hoach-nam";
export type PlanView = "list" | "save" | "history" | "version" | "entry" | "edit-entry" | "new-entry" | "programs" | "new-program";
export interface PlanRoute {
  base: string; year: number; view: PlanView; version?: number; ref?: string; parent?: string; kind?: "group" | "item"; tab?: KhtcTab;
  go: (href: string) => void;
}
export function planHref(base: string, year: number, view: PlanView = "list", version?: number): string {
  const tail = view === "save" ? "/luu-phien-ban" : view === "history" ? "/lich-su" : view === "version" ? `/phien-ban/${version}` : "";
  return `${base}${tail}?year=${year}`;
}
/** No Number/parseInt until a bounded canonical URL value has been checked. */
export function parsePlanRoute(path: string, search: URLSearchParams, currentYear: number): Omit<PlanRoute, "go"> | null {
  const base = path.match(/^\/kn\/(?:khtc|phong-ban\/[a-z_]+)\/ke-hoach-nam(?=\/|$)/)?.[0];
  if (!base || search.getAll("year").length > 1) return null;
  const rawYear = search.get("year") ?? String(currentYear);
  if (!/^(20\d{2}|2100)$/.test(rawYear)) return null;
  const year = Number(rawYear);
  const tail = path.slice(base.length);
  if (tail === "" || tail === "/") return { base, year, view: "list" };
  if (tail === "/luu-phien-ban") return { base, year, view: "save" };
  if (tail === "/lich-su") return { base, year, view: "history" };
  const tabRaw=search.get("tab")??"programs";
  const tab=TABS.find(t=>t===tabRaw);
  if (["tab","parent","kind"].some(k=>search.getAll(k).length>1)) return null;
  if (tail==="/chon-chuong-trinh" || tail==="/chuong-trinh-moi") return tab ? {base,year,tab,view:tail==="/chon-chuong-trinh"?"programs":"new-program"} : null;
  const uuid="[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}";
  if (tail==="/them-noi-dung") {
    const parent=search.get("parent")??""; const kind=search.get("kind");
    return tab && new RegExp(`^${uuid}$`).test(parent) && (kind==="group"||kind==="item") ? {base,year,tab,parent,kind,view:"new-entry"} : null;
  }
  const entryMatch=new RegExp(`^/noi-dung/(${uuid})(/sua)?$`).exec(tail);
  if(entryMatch)return {base,year,ref:entryMatch[1],view:entryMatch[2]?"edit-entry":"entry"};
  const match = /^\/phien-ban\/([1-9][0-9]{0,9})$/.exec(tail);
  if (!match) return null;
  const version = Number(match[1]);
  return version <= 2147483647 ? { base, year, view: "version", version } : null;
}

export function entryHref(base:string,year:number,ref:string,edit=false) {return `${base}/noi-dung/${ref}${edit?"/sua":""}?year=${year}`;}
export function newEntryHref(base:string,year:number,tab:KhtcTab,parent:string,kind:"group"|"item") {return `${base}/them-noi-dung?${new URLSearchParams({year:String(year),tab,parent,kind})}`;}
export function programHref(base:string,year:number,tab:KhtcTab,create=false) {return `${base}/${create?"chuong-trinh-moi":"chon-chuong-trinh"}?year=${year}&tab=${tab}`;}
