package backup

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// BackupOperationsTotal counts completed backup and restore operations.
	BackupOperationsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dr_syncer_backup_operations_total",
			Help: "Total number of backup and restore operations",
		},
		[]string{"operation_type", "status", "data_access"},
	)

	// BackupBytesTotal counts total bytes transferred in backup/restore operations.
	BackupBytesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dr_syncer_backup_bytes_total",
			Help: "Total bytes transferred during backup and restore operations",
		},
		[]string{"operation_type"},
	)

	// BackupDurationSeconds tracks duration of backup/restore operations.
	BackupDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dr_syncer_backup_duration_seconds",
			Help:    "Duration of backup and restore operations in seconds",
			Buckets: prometheus.ExponentialBuckets(1, 2, 15), // 1s to ~4.5 hours
		},
		[]string{"operation_type", "status", "data_access"},
	)

	// BackupSizeBytes tracks the size distribution of backup/restore operations.
	BackupSizeBytes = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dr_syncer_backup_size_bytes",
			Help:    "Size of backup and restore operations in bytes",
			Buckets: prometheus.ExponentialBuckets(1024, 4, 12), // 1KB to ~4GB
		},
		[]string{"operation_type"},
	)

	// BackupOperationsInProgress tracks currently active backup/restore operations.
	BackupOperationsInProgress = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dr_syncer_backup_operations_in_progress",
			Help: "Number of backup and restore operations currently in progress",
		},
		[]string{"operation_type"},
	)

	// BackupStandbyPVCCount tracks the number of managed standby PVCs.
	BackupStandbyPVCCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dr_syncer_backup_standby_pvc_count",
			Help: "Number of standby PVCs managed by dr-syncer",
		},
		[]string{"namespace", "state"},
	)

	// BackupLastSuccessfulTimestamp records the last successful backup time per PVC.
	BackupLastSuccessfulTimestamp = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dr_syncer_backup_last_successful_backup_timestamp",
			Help: "Unix timestamp of the last successful backup for a PVC",
		},
		[]string{"namespace", "pvc_name"},
	)

	// BackupRepositorySizeBytes tracks the repository size.
	BackupRepositorySizeBytes = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dr_syncer_backup_repository_size_bytes",
			Help: "Total size of the backup repository in bytes",
		},
		[]string{"repository_name"},
	)

	// BackupRepositorySnapshotCount tracks snapshot count per repository.
	BackupRepositorySnapshotCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dr_syncer_backup_repository_snapshot_count",
			Help: "Number of snapshots in the backup repository",
		},
		[]string{"repository_name"},
	)

	// BackupStandbyPVCResizeTotal counts standby PVC resize attempts.
	BackupStandbyPVCResizeTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dr_syncer_backup_standby_pvc_resize_total",
			Help: "Total number of standby PVC resize attempts due to source PVC growth",
		},
		[]string{"namespace", "status"},
	)
)

func init() {
	metrics.Registry.MustRegister(
		BackupOperationsTotal,
		BackupBytesTotal,
		BackupDurationSeconds,
		BackupSizeBytes,
		BackupOperationsInProgress,
		BackupStandbyPVCCount,
		BackupLastSuccessfulTimestamp,
		BackupRepositorySizeBytes,
		BackupRepositorySnapshotCount,
		BackupStandbyPVCResizeTotal,
	)
}

// RecordBackupStart records the start of a backup or restore operation.
func RecordBackupStart(operationType string) {
	BackupOperationsInProgress.WithLabelValues(operationType).Inc()
}

// RecordBackupComplete records a successful backup or restore operation.
func RecordBackupComplete(operationType, dataAccess string, durationSeconds float64, bytesTransferred int64) {
	BackupOperationsInProgress.WithLabelValues(operationType).Dec()
	BackupOperationsTotal.WithLabelValues(operationType, "success", dataAccess).Inc()
	BackupDurationSeconds.WithLabelValues(operationType, "success", dataAccess).Observe(durationSeconds)
	if bytesTransferred > 0 {
		BackupBytesTotal.WithLabelValues(operationType).Add(float64(bytesTransferred))
		BackupSizeBytes.WithLabelValues(operationType).Observe(float64(bytesTransferred))
	}
}

// RecordBackupFailure records a failed backup or restore operation.
func RecordBackupFailure(operationType, dataAccess string, durationSeconds float64) {
	BackupOperationsInProgress.WithLabelValues(operationType).Dec()
	BackupOperationsTotal.WithLabelValues(operationType, "failure", dataAccess).Inc()
	BackupDurationSeconds.WithLabelValues(operationType, "failure", dataAccess).Observe(durationSeconds)
}

// RecordLastSuccessfulBackup records the timestamp of the last successful backup for a PVC.
func RecordLastSuccessfulBackup(namespace, pvcName string) {
	BackupLastSuccessfulTimestamp.WithLabelValues(namespace, pvcName).Set(float64(time.Now().Unix()))
}

// RecordStandbyPVCCreated increments the standby PVC count for the "created" state.
func RecordStandbyPVCCreated(namespace string) {
	BackupStandbyPVCCount.WithLabelValues(namespace, "created").Inc()
}

// RecordStandbyPVCDeleted decrements the standby PVC count for the "created" state.
func RecordStandbyPVCDeleted(namespace string) {
	BackupStandbyPVCCount.WithLabelValues(namespace, "created").Dec()
}

// RecordStandbyPVCResized records a successful standby PVC resize.
func RecordStandbyPVCResized(namespace string) {
	BackupStandbyPVCResizeTotal.WithLabelValues(namespace, "success").Inc()
}

// RecordStandbyPVCResizeFailed records a failed standby PVC resize attempt.
func RecordStandbyPVCResizeFailed(namespace string) {
	BackupStandbyPVCResizeTotal.WithLabelValues(namespace, "failure").Inc()
}

// RecordRepositoryStats updates the repository metrics from BackupRepository status.
func RecordRepositoryStats(repoName string, sizeBytes int64, snapshotCount int32) {
	BackupRepositorySizeBytes.WithLabelValues(repoName).Set(float64(sizeBytes))
	BackupRepositorySnapshotCount.WithLabelValues(repoName).Set(float64(snapshotCount))
}
