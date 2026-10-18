package port

import "context"

// FileAuthorizer bổ sung quyền theo từng tệp khi chủ thể là một vùng chứa
// (ví dụ thư mục). Evidence gọi khi đọc, sau Resolver và trước khi cấp tệp.
// Attach chỉ dùng Resolver để cho phép nạp tệp trước khi ghi bản ghi nghiệp vụ.
type FileAuthorizer interface {
	AuthorizeFile(context.Context, File) (bool, error)
}
