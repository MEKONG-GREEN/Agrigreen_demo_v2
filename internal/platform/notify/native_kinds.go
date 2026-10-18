package notify

import "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"

// nativeNotificationKinds excludes data-only sync_now, which never creates a notification row.
func nativeNotificationKinds() []string {
	return []string{
		port.KindAlert,
		port.KindDocument,
		port.KindSyncConflict,
		port.KindOrgDeactivated,
		port.KindHouseholdRegistered,
		port.KindHouseholdMembershipChanged,
		port.KindFarmLogPending,
		port.KindFarmLogReviewed,
		port.KindSeasonOpened,
		port.KindIntegrationDeadLettered,
		port.KindAreaFeedStale,
		port.KindCollectionSchedule,
		port.KindCollectionStatement,
		port.KindMRVUrgent,
		port.KindMRVAnomaly,
		port.KindMRVOverdue,
		port.KindEgressAnomaly,
		port.KindAccountDormant,
	}
}
