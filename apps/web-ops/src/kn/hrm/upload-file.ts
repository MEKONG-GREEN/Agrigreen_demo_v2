import { ApiProblem } from "@/api/problem";

// Cùng giới hạn/chữ ký với hrm/domain/docs.go; máy chủ vẫn kiểm lại trước ghi.
export const HRM_MAX_DOC_BYTES = 15 * 1024 * 1024 - 1024;
const formats: Record<string, readonly [string, readonly number[]]> = {
  pdf: ["application/pdf", [37, 80, 68, 70, 45]],
  jpg: ["image/jpeg", [255, 216, 255]],
  jpeg: ["image/jpeg", [255, 216, 255]],
  png: ["image/png", [137, 80, 78, 71]],
  docx: ["application/vnd.openxmlformats-officedocument.wordprocessingml.document", [80, 75, 3, 4]],
  xlsx: ["application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", [80, 75, 3, 4]],
  doc: ["application/msword", [208, 207, 17, 224]],
  xls: ["application/vnd.ms-excel", [208, 207, 17, 224]],
};

function invalid(detail: string, code = "hrm.doc_format", status = 422): ApiProblem {
  return new ApiProblem({ type: "about:blank", title: "Tệp không hợp lệ", status, code, detail });
}

/** Đọc/kiểm trước mọi ghi, chuẩn MIME và bỏ lastModified (không phải dữ liệu nghiệp vụ).
 * Chọn lại cùng byte/tên sau mất phản hồi vì vậy vẫn có đúng thân multipart cũ.
 */
export async function prepareHrmFile(file: File): Promise<File> {
  const name = file.name.replaceAll("\\", "/").split("/").at(-1)?.trim() ?? "";
  const format = formats[name.split(".").at(-1)?.toLowerCase() ?? ""];
  if (!format || !name || [...name].length > 200 || /[\u0000-\u001f\u007f]/.test(name)) throw invalid("Tên hoặc định dạng tệp không được nhận.");
  if (file.size > HRM_MAX_DOC_BYTES) throw invalid("Tệp vượt dung lượng cho phép.", "hrm.doc_too_large", 413);
  if (file.size <= 0) throw invalid("Tệp rỗng.");
  let bytes: ArrayBuffer;
  try { bytes = await file.arrayBuffer(); } catch { throw invalid("Không đọc được tệp; vui lòng chọn lại tệp."); }
  const head = new Uint8Array(bytes);
  if (bytes.byteLength !== file.size || !format[1].every((b, i) => head[i] === b)) throw invalid("Nội dung tệp không đúng định dạng.");
  return new File([bytes], name, { type: format[0], lastModified: 0 });
}
