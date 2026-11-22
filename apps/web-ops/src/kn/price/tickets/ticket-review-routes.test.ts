import { describe, expect, it } from "vitest";
import { calendarDay, reviewFilters, reviewHref, reviewRoute } from "./ticket-review-routes";
const id = "0192a5a0-0000-7000-8000-00000000a501";
describe("204 ticket routes", () => {
 it("validates ID, action, depth; never falls back to a different ticket", () => {
  expect(reviewRoute([])).toEqual({ kind: "list" });
  for (const [tail, action] of [[undefined,"view"],["hieu-chinh","correct"],["doi-ngay","redate"],["xoa","delete"]]) expect(reviewRoute(["phieu",id,...(tail?[tail]:[])])).toEqual({ kind:"ticket",id,action });
  for (const p of [["phieu"],["phieu","bad"],["phieu",id,"unknown"],["phieu",id,"xoa","extra"],["other",id],["phieu",`${id}?x=1`]]) expect(reviewRoute(p)).toEqual({kind:"invalid"});
 });
 it("keeps canonical list filters and safely validates calendar dates", () => {
  const f = reviewFilters(new URLSearchParams("day=2026-10-12&mine=false&tab=alerts&return=https://evil"),"2026-10-13");
  expect(reviewHref(f,id,"correct")).toBe(`/kn/bao-gia/kiem-duyet/phieu/${id}/hieu-chinh?day=2026-10-12&mine=false&tab=alerts`);
  expect(reviewHref(f)).toBe("/kn/bao-gia/kiem-duyet?day=2026-10-12&mine=false&tab=alerts");
  expect(calendarDay("2026-02-30")).toBe(false);expect(calendarDay("2028-02-29")).toBe(true);
  expect(reviewFilters(new URLSearchParams("day=bad&mine=yes&tab=bad"),"2026-10-13")).toEqual({day:"2026-10-13",mine:true,tab:"tickets"});
 });
});
