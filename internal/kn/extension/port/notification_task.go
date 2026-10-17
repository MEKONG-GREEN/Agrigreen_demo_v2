package port

// Notification kinds owned by task workflows.
const (
	KindTaskEscalated = "task_escalated" // nhiệm vụ leo thang lên tỉnh (KN US-TASK-02)
	// KindTaskAssigned: nhiệm vụ tự sinh từ yêu cầu hỗ trợ App HTX → cán bộ được giao (KN US-TASK-01).
	KindTaskAssigned = "task_assigned"
)
