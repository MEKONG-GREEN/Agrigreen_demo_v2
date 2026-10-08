// Chốt chặn cấu hình theo môi trường triển khai (M16/M17). Dùng chung cho next.config.ts (chặn
// lúc `next build`) và config.ts (chốt lại lúc chạy trong trình duyệt). Không import gì để
// next.config.ts nạp được trực tiếp.
//
// NEXT_PUBLIC_DEPLOY_ENV (nhúng lúc build):
//   production (mặc định khi bỏ trống) | staging — bản thật: CẤM giả lập, URL bắt buộc + https.
//   demo — bản trình diễn (Render, E2E): được bật NEXT_PUBLIC_API_MOCK=1.
//   dev  — máy lập trình/Compose cục bộ: cho phép mặc định localhost, http và giả lập.
// Bản không phải production (next dev, vitest) không bị kiểm.

export const DEPLOY_ENVS = ["production", "staging", "demo", "dev"] as const;
export type DeployEnv = (typeof DEPLOY_ENVS)[number];

/** Biến môi trường liên quan — truyền tường minh vì trình duyệt chỉ có giá trị Next.js đã nhúng. */
export type PublicEnv = Readonly<Record<string, string | undefined>>;

export function deployEnvOf(env: PublicEnv): DeployEnv | null {
  const raw = (env.NEXT_PUBLIC_DEPLOY_ENV ?? "").trim();
  if (raw === "") return "production";
  return (DEPLOY_ENVS as readonly string[]).includes(raw) ? (raw as DeployEnv) : null;
}

/** Bản build production (NODE_ENV=production) thì mới kiểm; dev/test giữ mặc định localhost. */
export function isProductionBuild(env: PublicEnv): boolean {
  return env.NODE_ENV === "production";
}

/** Giả lập (bỏ qua đăng nhập) chỉ được bật ở dev/test, hoặc bản build có DEPLOY_ENV demo/dev. */
export function isMockEnabled(env: PublicEnv): boolean {
  if (env.NEXT_PUBLIC_API_MOCK !== "1") return false;
  if (!isProductionBuild(env)) return true;
  const d = deployEnvOf(env);
  return d === "demo" || d === "dev";
}

/** Có dùng giá trị mặc định localhost cho dev không (production thật thì không bao giờ). */
export function allowsDevDefaults(env: PublicEnv): boolean {
  return !isProductionBuild(env) || deployEnvOf(env) === "dev";
}

export interface EnvRules {
  /** URL bắt buộc (trừ khi dev hoặc demo giả lập) và phải https. */
  requiredUrls: readonly string[];
  /** URL tùy chọn: nếu đặt thì phải https. */
  optionalUrls?: readonly string[];
}

/** Danh sách lỗi cấu hình của một bản build production; rỗng = hợp lệ. */
export function buildEnvErrors(env: PublicEnv, rules: EnvRules): string[] {
  const errors: string[] = [];
  const deploy = deployEnvOf(env);
  if (deploy === null) {
    return [`NEXT_PUBLIC_DEPLOY_ENV="${env.NEXT_PUBLIC_DEPLOY_ENV}" không hợp lệ (chỉ: ${DEPLOY_ENVS.join(", ")}).`];
  }
  const mock = env.NEXT_PUBLIC_API_MOCK === "1";
  if (mock && deploy !== "demo" && deploy !== "dev") {
    errors.push(
      `NEXT_PUBLIC_API_MOCK=1 bỏ qua đăng nhập — chỉ cho phép khi NEXT_PUBLIC_DEPLOY_ENV=demo hoặc dev (hiện: ${deploy}).`,
    );
  }
  if (deploy === "dev" || (mock && deploy === "demo")) return errors;
  for (const key of rules.requiredUrls) {
    const value = (env[key] ?? "").trim();
    if (value === "") errors.push(`${key} bắt buộc cho bản build ${deploy} (không có mặc định localhost).`);
    else if (!isHttps(value)) errors.push(`${key} phải là URL https:// cho bản build ${deploy} (hiện: ${value}).`);
  }
  for (const key of rules.optionalUrls ?? []) {
    const value = (env[key] ?? "").trim();
    if (value !== "" && !isHttps(value)) errors.push(`${key} phải là URL https:// cho bản build ${deploy} (hiện: ${value}).`);
  }
  return errors;
}

/** Gọi trong next.config.ts ở pha build: ném lỗi để `next build` dừng. */
export function assertBuildEnv(env: PublicEnv, rules: EnvRules): void {
  const errors = buildEnvErrors(env, rules);
  if (errors.length > 0) {
    throw new Error(
      `Cấu hình build không hợp lệ:\n  - ${errors.join("\n  - ")}\n` +
        `Bản thật: đặt đủ NEXT_PUBLIC_* (https). Bản demo giả lập: npm run build:mock. Máy cục bộ: NEXT_PUBLIC_DEPLOY_ENV=dev.`,
    );
  }
}

function isHttps(value: string): boolean {
  try {
    return new URL(value).protocol === "https:";
  } catch {
    return false;
  }
}
