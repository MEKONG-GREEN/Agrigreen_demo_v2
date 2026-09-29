/**
 * Dump golden cases from the mekong-green prototype (executable spec) for the
 * Python optimizer. Produces, per case: snapshot.json (optimizer input format,
 * de-identified) and expected.json (prototype ScenarioResult, raw).
 *
 * Run: node dump.ts <outDir>
 */
process.env.SUPER_ADMIN_PASSWORD ??= 'KiemThu-SAdmin-2026';
process.env.DEMO_ACCOUNT_PASSWORD ??= '123456';
process.env.DATA_ENCRYPTION_KEY ??= '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
import { mkdtempSync, mkdirSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const P = (process.env.MEKONG_GREEN_DIR ?? '../mekong-green') + '/src';
const dbm = await import(`file:///${P}/platform/db/db.ts`);
dbm.configureDatabase(join(mkdtempSync(join(tmpdir(), 'mg-golden-')), 'golden.db'));
const { migrate } = await import(`file:///${P}/platform/db/schema.ts`);
const { seedAll } = await import(`file:///${P}/seed.ts`);
const params = await import(`file:///${P}/erp/params/store.ts`);
const { ALL_PARAMETERS } = await import(`file:///${P}/erp/params/catalog.ts`);
const sim = await import(`file:///${P}/erp/simulation/service.ts`);
const { ENGINE_VERSION } = await import(`file:///${P}/erp/simulation/engine.ts`);
const { buildHarvestCalendar } = await import(`file:///${P}/erp/simulation/flow.ts`);
const { DistanceService } = await import(`file:///${P}/platform/geo/distance.ts`);
const { haversineKm } = await import(`file:///${P}/platform/geo/geo.ts`);

migrate();
seedAll();
const actor = { name: 'golden' };
const out = process.argv[2];

function seasonType(code: string): string { return code.split('-')[0].toUpperCase(); }
const MONTH_START = [0, 1, 32, 60, 91, 121, 152, 182, 213, 244, 274, 305, 335];
const MONTH_LENGTH = [0, 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
function doyOf(iso: string): number | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})/.exec(iso);
  if (!m) return null;
  const month = Number(m[2]); const day = Number(m[3]);
  if (month < 1 || month > 12) return null;
  if (day < 1 || day > MONTH_LENGTH[month]) {
    if (month === 2 && day === 29) return MONTH_START[2] + 27;
    return null;
  }
  return MONTH_START[month] + day - 1;
}

function textKey(k: string): string { return k === 'balePress' ? 'bale_press' : k; }

function paramEntries(version: number) {
  const records = params.parameterSetSnapshot(version) as any[];
  const byCode = new Map(records.map((r) => [r.code, r]));
  return ALL_PARAMETERS.map((def: any) => {
    const r = byCode.get(def.code);
    const value = r ? r.value_base : def.base;
    const tv = r?.text_value ? JSON.parse(r.text_value) : (def.textValue ?? null);
    const text = tv ? Object.fromEntries(Object.entries(tv).map(([k, v]) => [textKey(k), String(v)])) : null;
    const approved = r ? Boolean(r.approved_by) : false;
    return {
      code: def.code, number: def.number, name: def.name, classification: def.classification,
      value: value === null || value === undefined ? null : String(value),
      text_value: text,
      min: (r?.value_min ?? def.min) == null ? null : String(r?.value_min ?? def.min),
      max: (r?.value_max ?? def.max) == null ? null : String(r?.value_max ?? def.max),
      approved,
      param_version: approved ? 1 : null,
    };
  });
}

function dumpCase(name: string, scenarioId: string, withSensitivity: boolean) {
  const scenario = dbm.one('SELECT * FROM scenarios WHERE id = ?', [scenarioId]) as any;
  const result = sim.runSimulation(scenarioId, {}, actor);
  const version = result.parameterSetVersion;
  const p = params.resolveParams(version);
  const plant = dbm.one('SELECT id, name, lat, lng, annual_demand_tons FROM facilities WHERE id = ?', [scenario.plant_id]) as any;
  const hubs = dbm.all(`SELECT sh.id, sh.hub_id, sh.service_radius_km, sh.design_capacity_tons, sh.barge_payload_tons,
        sh.mode_field_hub, sh.mode_hub_plant, sh.land_mode, sh.sort_order, h.code, h.lat, h.lng
      FROM scenario_hubs sh JOIN candidate_hubs h ON h.id = sh.hub_id WHERE sh.scenario_id = ? ORDER BY sh.sort_order, h.code`, [scenarioId]) as any[];
  const coops = dbm.all("SELECT id, code, lat, lng, member_count FROM cooperatives WHERE status = 'active'") as any[];
  const stats = dbm.all(`SELECT hs.htx_id, hs.season_id, s.code AS season_code, s.sort_order, hs.paddy_tons, hs.planted_area_ha
      FROM harvest_statistics hs JOIN seasons s ON s.id = hs.season_id`) as any[];
  const seasons = dbm.all('SELECT id, code, name, start_month, end_month, sort_order FROM seasons') as any[];
  const progress = dbm.all(`SELECT s.code AS season_code, p.as_of_date AS day, SUM(p.output_tons) AS tons
       FROM commune_harvest_progress p JOIN commune_crop_seasons c ON c.id = p.commune_season_id
       JOIN seasons s ON s.id = c.season_id WHERE p.output_tons > 0 GROUP BY s.code, p.as_of_date`) as any[];
  const harvestDates = dbm.all(`SELECT s.code AS season_code, c.harvest_date AS day, SUM(c.output_tons) AS tons
       FROM commune_crop_seasons c JOIN seasons s ON s.id = c.season_id
      WHERE c.harvest_date IS NOT NULL AND c.output_tons > 0 GROUP BY s.code, c.harvest_date`) as any[];
  // Same selection rule as buildHarvestCalendar: progress rows first, harvest dates only for types without progress.
  const obs: Record<string, { day_of_year: number; tons: string }[]> = {};
  const push = (row: any) => {
    const doy = doyOf(row.day);
    if (doy === null || !(row.tons > 0)) return;
    const t = seasonType(row.season_code);
    (obs[t] ??= []).push({ day_of_year: doy, tons: String(row.tons) });
  };
  progress.forEach(push);
  // Prototype quirk: `if (byType.has(type)) continue; collect([row])` — after the first harvest-date
  // row of a type is collected the type exists, so only the FIRST harvest date per type is used.
  for (const row of harvestDates) if (!(seasonType(row.season_code) in obs)) push(row);

  const ds = new DistanceService({ correctionFactors: p.correctionFactors, waterwayAccessRadiusKm: 8, minBargeLoadTons: p.bargePayloadTons });
  const plantPt = { lat: plant.lat, lng: plant.lng };
  const routes: any[] = [];
  const seen = new Set<string>();
  const addRoute = (fromRef: string, from: any, toRef: string, to: any) => {
    for (const mode of ['road', 'waterway']) {
      const key = `${fromRef}|${toRef}|${mode}`;
      if (seen.has(key)) continue;
      seen.add(key);
      const r = ds.distance(from, to, mode);
      const entry: any = { from: fromRef, to: toRef, mode, distance_km: String(r.distanceKm), source: r.source };
      if (r.source === 'haversine_hieu_chinh') entry.straight_km = String(haversineKm(from, to));
      if (mode === 'waterway') entry.waterway_access = ds.hasWaterwayAccess(from, to);
      routes.push(entry);
    }
  };
  const radius: any[] = [];
  const inAny = new Set<string>();
  for (const hub of hubs) {
    const hp = { lat: hub.lat, lng: hub.lng };
    addRoute(`hub:${hub.hub_id}`, hp, 'plant', plantPt);
    if (hub.service_radius_km <= 0) continue;
    for (const c of coops) {
      if (c.lat === null || c.lng === null) continue;
      const cp = { lat: c.lat, lng: c.lng };
      const rk = haversineKm(hp, cp);
      if (rk > hub.service_radius_km) continue;
      radius.push({ cooperative_ref: c.id, hub_ref: hub.hub_id, radius_km: String(rk) });
      inAny.add(c.id);
      addRoute(`coop:${c.id}`, cp, `hub:${hub.hub_id}`, hp);
    }
  }
  for (const c of coops) if (inAny.has(c.id)) addRoute(`coop:${c.id}`, { lat: c.lat, lng: c.lng }, 'plant', plantPt);

  const statsBy = new Map<string, any[]>();
  for (const s of stats) (statsBy.get(s.htx_id) ?? statsBy.set(s.htx_id, []).get(s.htx_id)!).push(s);
  const snapshot = {
    schema_version: 1,
    snapshot_id: `00000000-0000-7000-8000-${String(Object.keys(cases).length + 1).padStart(12, '0')}`,
    simulation_id: `00000000-0000-7000-9000-${String(Object.keys(cases).length + 1).padStart(12, '0')}`,
    tenant_ref: '00000000-0000-7000-a000-000000000001',
    algorithm_version: `prototype-${ENGINE_VERSION}`,
    network_version: 'prototype-transport-routes',
    created_at: '2026-09-29T00:00:00Z',
    scenario: {
      scenario_id: scenario.id, code: scenario.code,
      lifecycle_years: scenario.lifecycle_years, discounted: scenario.discounted === 1,
      baseline_mode: scenario.baseline_mode,
      baseline_manual_cost_per_ton: scenario.baseline_manual_cost_per_ton == null ? null : String(scenario.baseline_manual_cost_per_ton),
      baseline_scenario_cost_per_ton: null,
      plant: { plant_ref: plant.id, annual_demand_tons: plant.annual_demand_tons ? String(plant.annual_demand_tons) : null },
    },
    hubs: hubs.map((h) => ({
      scenario_hub_id: h.id, hub_ref: h.hub_id, code: h.code, sort_order: h.sort_order,
      service_radius_km: String(h.service_radius_km), design_capacity_tons: String(h.design_capacity_tons),
      // Prototype quirk: engine.ts's scenario_hubs SELECT omits barge_payload_tons, so the per-hub barge
      // is ALWAYS params.bargeSmallPayloadTons. Record the effective input (null) for golden parity.
      barge_payload_tons: null,
      mode_field_hub: h.mode_field_hub, mode_hub_plant: h.mode_hub_plant, land_mode: h.land_mode,
    })),
    cooperatives: coops.filter((c) => c.lat !== null && c.lng !== null).map((c) => ({
      cooperative_ref: c.id, member_count: c.member_count ?? 0,
      seasons: (statsBy.get(c.id) ?? []).map((s) => ({ season_key: s.season_id, paddy_tons: String(s.paddy_tons), planted_area_ha: String(s.planted_area_ha) })),
    })),
    seasons: seasons.map((s) => ({ season_key: s.id, season_type: seasonType(s.code), end_month: s.end_month, sort_order: s.sort_order })),
    harvest_observations: obs,
    radius,
    routes,
    params: {
      set_version: `prototype-v${version}`,
      all_approved: p.unapproved.length === 0,
      unapproved: p.unapproved.map((u: any) => ({ code: u.code, number: u.number, name: u.name })),
      values: paramEntries(version),
    },
    options: { mode: 'evaluate', sensitivity_codes: withSensitivity ? sim.DEFAULT_SENSITIVITY_CODES : [] },
  };
  const cal = buildHarvestCalendar(p);
  const calendar = [...cal.values()].map((c: any) => ({ season_key: c.seasonId, source: c.source, days: c.days }));
  let sensitivity = null;
  if (withSensitivity) sensitivity = sim.runSensitivity(scenarioId, sim.DEFAULT_SENSITIVITY_CODES, actor);
  const dir = join(out, name);
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, 'snapshot.json'), JSON.stringify(snapshot, null, 1));
  writeFileSync(join(dir, 'expected.json'), JSON.stringify({ engine_version: ENGINE_VERSION, result, calendar, sensitivity }, null, 1));
  cases[name] = scenarioId;
  console.log(name, 'hubs', hubs.length, 'routes', routes.length, 'delivered', result.deliveredTons, 'tco/t', result.financial.tcoPerTon);
}
const cases: Record<string, string> = {};

// Case 1: simulation.test.ts setup — 2 hubs, radius 40, capacity 60 000, params unapproved.
const hubA = sim.createCandidateHub({ name: 'Hub test A', lat: 10.3800, lng: 105.4350 }, actor);
const hubB = sim.createCandidateHub({ name: 'Hub test B', lat: 10.4600, lng: 105.6400 }, actor);
const s1 = sim.createScenario({ name: 'Kịch bản kiểm thử', lifecycleYears: 10 }, actor);
sim.attachHub(s1.id, hubA.id, { serviceRadiusKm: 40, designCapacityTons: 60_000 }, actor);
sim.attachHub(s1.id, hubB.id, { serviceRadiusKm: 40, designCapacityTons: 60_000 }, actor);
dumpCase('case1_two_hubs_unapproved', s1.id, false);

// Case 2: demo "Phương án 3 Hub", all assumptions approved, discounted, with sensitivity (FN-18).
params.approveAllAssumptions('Ban lãnh đạo', actor);
const hubC = sim.createCandidateHub({ name: 'Hub Cần Thơ', lat: 10.0330, lng: 105.7830 }, actor);
const s2 = sim.createScenario({ name: 'Phương án 3 Hub', lifecycleYears: 10, discounted: true }, actor);
for (const h of [hubA, hubC, hubB]) sim.attachHub(s2.id, h.id, { serviceRadiusKm: 35, designCapacityTons: 70_000 }, actor);
dumpCase('case2_three_hubs_approved_discounted', s2.id, true);

// Case 3: land lease, 2 000 t barge, waterway overrides, manual baseline, 5 hubs radius 28.
const hubD = sim.createCandidateHub({ name: 'Hub Mỹ Tho', lat: 10.3500, lng: 106.3600 }, actor);
const hubE = sim.createCandidateHub({ name: 'Hub Bến Lức', lat: 10.7200, lng: 106.4000 }, actor);
const s3 = sim.createScenario({ name: 'Phương án 5 Hub', lifecycleYears: 12 }, actor);
for (const h of [hubA, hubC, hubB, hubD, hubE]) {
  sim.attachHub(s3.id, h.id, { serviceRadiusKm: 28, designCapacityTons: 45_000, landMode: 'thue', bargePayloadTons: 2_000, modeHubPlant: 'waterway', modeFieldHub: h === hubD ? 'waterway' : 'auto' }, actor);
}
sim.updateScenario(s3.id, { baseline_mode: 'manual', baseline_manual_cost_per_ton: 950_000 }, actor);
dumpCase('case3_five_hubs_lease_manual_baseline', s3.id, false);

// Case 4: tiny capacity → overflow / days at capacity warnings.
const s4 = sim.createScenario({ name: 'Kho nhỏ', lifecycleYears: 10 }, actor);
sim.attachHub(s4.id, hubB.id, { serviceRadiusKm: 45, designCapacityTons: 5_000 }, actor);
dumpCase('case4_small_capacity_overflow', s4.id, false);

// Case 5: observed harvest curves — DX from weekly progress rows, HT from harvest dates only.
{
  const seasonsRows = dbm.all('SELECT id, code FROM seasons') as any[];
  const dx = seasonsRows.find((s: any) => s.code.toUpperCase().startsWith('DX'));
  const ht = seasonsRows.find((s: any) => s.code.toUpperCase().startsWith('HT'));
  const csA = 'golden-cs-a'; const csB = 'golden-cs-b'; const csC = 'golden-cs-c';
  dbm.insert('commune_crop_seasons', { id: csA, season_id: dx.id, commune: 'X1', area_ha: 100, output_tons: 1, updated_at: 'x' });
  dbm.insert('commune_crop_seasons', { id: csB, season_id: ht.id, commune: 'X2', area_ha: 100, harvest_date: '2025-07-20', output_tons: 5200, updated_at: 'x' });
  dbm.insert('commune_crop_seasons', { id: csC, season_id: ht.id, commune: 'X3', area_ha: 100, harvest_date: '2025-08-02', output_tons: 3100, updated_at: 'x' });
  const pts: [string, number][] = [['2025-02-24', 1800], ['2025-03-03', 5200], ['2025-03-10', 9100], ['2025-03-17', 7600], ['2025-03-31', 2400], ['2024-02-29', 300]];
  pts.forEach(([d, t], i) => dbm.insert('commune_harvest_progress', { id: `golden-p-${i}`, commune_season_id: csA, as_of_date: d, area_ha: 10, output_tons: t }));
  const s5 = sim.createScenario({ name: 'Lịch thu hoạch thực tế', lifecycleYears: 10 }, actor);
  sim.attachHub(s5.id, hubA.id, { serviceRadiusKm: 40, designCapacityTons: 30_000 }, actor);
  sim.attachHub(s5.id, hubC.id, { serviceRadiusKm: 40, designCapacityTons: 30_000, bargePayloadTons: 2_000 }, actor);
  dumpCase('case5_observed_harvest_calendar', s5.id, false);
}
