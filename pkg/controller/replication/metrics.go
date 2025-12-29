package replication

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// PVCSyncBytesTransferred tracks total bytes transferred during PVC syncs
	PVCSyncBytesTransferred = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dr_syncer_pvc_sync_bytes_transferred_total",
			Help: "Total bytes transferred during PVC sync operations",
		},
		[]string{"namespace", "pvc_name", "destination_namespace"},
	)

	// PVCSyncFilesTransferred tracks total files transferred during PVC syncs
	PVCSyncFilesTransferred = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dr_syncer_pvc_sync_files_transferred_total",
			Help: "Total files transferred during PVC sync operations",
		},
		[]string{"namespace", "pvc_name", "destination_namespace"},
	)

	// PVCSyncProgress tracks current sync progress percentage (0-100)
	PVCSyncProgress = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dr_syncer_pvc_sync_progress_percent",
			Help: "Current progress percentage of PVC sync operation (0-100)",
		},
		[]string{"namespace", "pvc_name", "destination_namespace"},
	)

	// PVCSyncDuration tracks sync duration in seconds
	PVCSyncDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "dr_syncer_pvc_sync_duration_seconds",
			Help:    "Duration of PVC sync operations in seconds",
			Buckets: prometheus.ExponentialBuckets(1, 2, 15), // 1s to ~9 hours
		},
		[]string{"namespace", "pvc_name", "destination_namespace", "status"},
	)

	// PVCSyncOperations tracks total sync operations
	PVCSyncOperations = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "dr_syncer_pvc_sync_operations_total",
			Help: "Total number of PVC sync operations",
		},
		[]string{"namespace", "pvc_name", "destination_namespace", "status"},
	)

	// PVCSyncSpeed tracks current sync speed in bytes per second
	PVCSyncSpeed = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "dr_syncer_pvc_sync_speed_bytes_per_second",
			Help: "Current sync speed in bytes per second",
		},
		[]string{"namespace", "pvc_name", "destination_namespace"},
	)
)

func init() {
	// Register metrics with the controller-runtime metrics registry
	metrics.Registry.MustRegister(
		PVCSyncBytesTransferred,
		PVCSyncFilesTransferred,
		PVCSyncProgress,
		PVCSyncDuration,
		PVCSyncOperations,
		PVCSyncSpeed,
	)
}

// RecordSyncStart records the start of a sync operation
func RecordSyncStart(namespace, pvcName, destNamespace string) {
	PVCSyncProgress.WithLabelValues(namespace, pvcName, destNamespace).Set(0)
	PVCSyncSpeed.WithLabelValues(namespace, pvcName, destNamespace).Set(0)
}

// RecordSyncProgress records sync progress metrics
func RecordSyncProgress(namespace, pvcName, destNamespace string, bytesTransferred int64, filesTransferred int, progress int, speedBytesPerSec float64) {
	PVCSyncProgress.WithLabelValues(namespace, pvcName, destNamespace).Set(float64(progress))
	PVCSyncSpeed.WithLabelValues(namespace, pvcName, destNamespace).Set(speedBytesPerSec)
}

// RecordSyncComplete records completion of a sync operation
func RecordSyncComplete(namespace, pvcName, destNamespace string, bytesTransferred int64, filesTransferred int, durationSeconds float64, success bool) {
	status := "success"
	if !success {
		status = "failure"
	}

	PVCSyncBytesTransferred.WithLabelValues(namespace, pvcName, destNamespace).Add(float64(bytesTransferred))
	PVCSyncFilesTransferred.WithLabelValues(namespace, pvcName, destNamespace).Add(float64(filesTransferred))
	PVCSyncProgress.WithLabelValues(namespace, pvcName, destNamespace).Set(100)
	PVCSyncSpeed.WithLabelValues(namespace, pvcName, destNamespace).Set(0)
	PVCSyncDuration.WithLabelValues(namespace, pvcName, destNamespace, status).Observe(durationSeconds)
	PVCSyncOperations.WithLabelValues(namespace, pvcName, destNamespace, status).Inc()
}

// RecordSyncFailure records a failed sync operation
func RecordSyncFailure(namespace, pvcName, destNamespace string, durationSeconds float64) {
	PVCSyncProgress.WithLabelValues(namespace, pvcName, destNamespace).Set(0)
	PVCSyncSpeed.WithLabelValues(namespace, pvcName, destNamespace).Set(0)
	PVCSyncDuration.WithLabelValues(namespace, pvcName, destNamespace, "failure").Observe(durationSeconds)
	PVCSyncOperations.WithLabelValues(namespace, pvcName, destNamespace, "failure").Inc()
}
