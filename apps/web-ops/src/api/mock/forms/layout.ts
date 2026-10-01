// BẢN PORT TS (CHỈ CHO BACKEND GIẢ LẬP) của reports/form/layout.go: nhãn kỳ theo đợt, {season}.
import type { FormDefinition } from "@/kn/forms/types";
import { cloneDef } from "./ops";

export interface LayoutOptions {
  periodLabels: string[];
  season: string;
}

const season = (s: string, v: string) => (v ? s.replaceAll("{season}", v) : s);

export function layout(def: FormDefinition, o: LayoutOptions): FormDefinition {
  const d = cloneDef(def);
  const blocks = new Map(d.period_blocks.map((b) => [b.id, b]));
  const label = (block: string, slot: number): string | null => {
    if (!o.periodLabels.length) {
      const b = blocks.get(block);
      if (!b?.title_pattern) return null;
      return b.title_pattern.replaceAll("{n}", String(slot));
    }
    return slot <= o.periodLabels.length ? (o.periodLabels[slot - 1] ?? null) : null;
  };
  for (const c of d.columns) {
    if (c.kind === "per_period") {
      if (o.periodLabels.length && (c.period_slot ?? 0) > o.periodLabels.length) c.inactive = true;
      else if (blocks.get(c.period_block ?? "")?.width === 1) {
        const l = label(c.period_block ?? "", c.period_slot ?? 0);
        if (l !== null) c.title = l;
      }
    }
    c.title = season(c.title, o.season);
  }
  for (const hc of d.header.cells) {
    if ((hc.period_slot ?? 0) > 0) {
      const l = label(hc.period_block ?? "", hc.period_slot ?? 0);
      if (l !== null) hc.text = l;
    }
    hc.text = season(hc.text, o.season);
  }
  d.title = season(d.title, o.season);
  d.sheet_name = season(d.sheet_name, o.season);
  return d;
}
