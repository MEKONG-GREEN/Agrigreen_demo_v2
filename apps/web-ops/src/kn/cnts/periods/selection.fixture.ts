import type { createCntsPeriodApi } from "@/kn/cnts/periods/api";
/** Fixture selection through the same public catalog used by the screen. */
export async function cntsSelection(api: ReturnType<typeof createCntsPeriodApi>, code: string) {
  const form = (await api.forms()).find(f => f.code === code);
  if (!form?.template_id || !form.template_version) throw new Error("Install and select an immutable CNTS template in the fixture");
  return { template_id: form.template_id, template_version: form.template_version };
}
