import { newIdempotencyKey, outcomeUnknown } from "@/api/idempotency";
import { ApiProblem } from "@/api/problem";
import type { ReportApi, ReportSave, ReportSaveReceipt } from "./api";

type Pending = { operationID: string; key: string; baseVersion: number };
type Snapshot = { ready: boolean; pending: Pending | null; receipt: ReportSaveReceipt | null; error: string; blocked: boolean };
const EMPTY: Snapshot = { ready: false, pending: null, receipt: null, error: "", blocked: false };
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
export const reportSaveStorageKey244 = (scope: string, id: string) => `kn:khtc:report-save:v1:${scope}:${id}`;

// Only operation metadata is persisted. No narrative, file bytes or cached
// authorization is retained; the backend checks current authority on every read.
export function createReportSaveSession244(api: Pick<ReportApi, "save" | "saveOperation">, id: string, actor: string, scope: string, storage: () => Storage | null) {
    let state = EMPTY, initialized = false;
    const listeners = new Set<() => void>();
    const key = reportSaveStorageKey244(scope, id);
    const publish = (next: Snapshot) => { state = next; for (const listener of listeners) listener(); };
    const persist = (pending: Pending | null) => {
        const store = storage();
        if (!store) throw new Error("Không lưu được mã đối chiếu của lần lưu. Chưa gửi thay đổi.");
        if (pending) store.setItem(key, JSON.stringify(pending)); else store.removeItem(key);
    };
    const verify = (receipt: ReportSaveReceipt, pending: Pending) => {
        if (receipt.operation_id !== pending.operationID || receipt.period_id !== id) throw new Error("Kết quả chưa khớp lần lưu cần đối chiếu.");
        if (receipt.outcome === "unknown" && !receipt.applied) return false;
        const applied = receipt.applied;
        if (receipt.outcome !== "applied" || !applied || applied.base_version !== pending.baseVersion || applied.version <= pending.baseVersion
            || !uuid.test(applied.draft_id) || applied.actor_ref !== actor || !/^[0-9a-f]{64}$/.test(applied.request_hash) || !Number.isFinite(Date.parse(applied.at))) {
            throw new Error("Kết quả chưa khớp lần lưu cần đối chiếu.");
        }
        return true;
    };
    const confirm = (receipt: ReportSaveReceipt, pending: Pending) => {
        if (!verify(receipt, pending)) return false;
        persist(null);
        publish({ ready: true, pending: null, receipt, error: "", blocked: false });
        return true;
    };
    return {
        subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener); }; },
        getSnapshot: () => state,
        getServerSnapshot: () => EMPTY,
        load() {
            if (initialized) return;
            initialized = true;
            if (!scope) { publish({ ...EMPTY, ready: true, blocked: true }); return; }
            try {
                const raw = storage()?.getItem(key);
                let pending: Pending | null = null;
                if (raw) {
                    if (raw.length > 1024) throw new Error("Mã đối chiếu không hợp lệ.");
                    const value = JSON.parse(raw) as Pending;
                    if (!uuid.test(value.operationID) || !uuid.test(value.key) || !Number.isSafeInteger(value.baseVersion) || value.baseVersion < 0) throw new Error("Mã đối chiếu không hợp lệ.");
                    pending = { operationID: value.operationID, key: value.key, baseVersion: value.baseVersion };
                }
                publish({ ...EMPTY, ready: true, pending });
            } catch { publish({ ...EMPTY, ready: true, blocked: true, error: "Không đọc được mã đối chiếu của lần lưu trước." }); }
        },
        async save(body: ReportSave) {
            if (!state.ready || state.blocked || state.pending) throw new Error("Đối chiếu lần lưu trước trước khi lưu tiếp.");
            const operationID = newIdempotencyKey();
            const pending = { operationID, key: operationID, baseVersion: body.base_version };
            persist(pending);
            publish({ ...state, pending, receipt: null, error: "" });
            try {
                const response = await api.save(id, { ...body, operation_id: operationID }, pending.key);
                if (!response.save_receipt || !confirm(response.save_receipt, pending)) throw new Error("Chưa xác định kết quả lưu. Cần đối chiếu lại.");
                return response.save_receipt;
            } catch (error) {
                if (error instanceof ApiProblem && !outcomeUnknown({ status: error.status, code: error.code })) {
                    persist(null);
                    publish({ ...state, pending: null });
                }
                throw error;
            }
        },
        async reconcile() {
            const pending = state.pending;
            if (!pending) return null;
            const receipt = await api.saveOperation(id, pending.operationID);
            if (!confirm(receipt, pending)) publish({ ...state, error: "Chưa xác định kết quả lần lưu. Tiếp tục đối chiếu trước thao tác tiếp theo." });
            return receipt;
        },
    };
}
