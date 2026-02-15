package backup

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func getCounterValue(counter prometheus.Counter) float64 {
	var m dto.Metric
	counter.Write(&m)
	return m.GetCounter().GetValue()
}

func getGaugeValue(gauge prometheus.Gauge) float64 {
	var m dto.Metric
	gauge.Write(&m)
	return m.GetGauge().GetValue()
}

func TestRecordBackupStart(t *testing.T) {
	before := getGaugeValue(BackupOperationsInProgress.WithLabelValues("backup"))
	RecordBackupStart("backup")
	after := getGaugeValue(BackupOperationsInProgress.WithLabelValues("backup"))

	if after != before+1 {
		t.Errorf("expected in-progress gauge to increment by 1, got delta=%f", after-before)
	}

	// Clean up: decrement back
	BackupOperationsInProgress.WithLabelValues("backup").Dec()
}

func TestRecordBackupComplete(t *testing.T) {
	// Set up: start an operation first
	RecordBackupStart("backup")

	beforeOps := getCounterValue(BackupOperationsTotal.WithLabelValues("backup", "success", "snapshot"))
	beforeBytes := getCounterValue(BackupBytesTotal.WithLabelValues("backup"))

	RecordBackupComplete("backup", "snapshot", 120.5, 1024*1024)

	afterOps := getCounterValue(BackupOperationsTotal.WithLabelValues("backup", "success", "snapshot"))
	afterBytes := getCounterValue(BackupBytesTotal.WithLabelValues("backup"))

	if afterOps != beforeOps+1 {
		t.Errorf("expected operations counter to increment by 1, got delta=%f", afterOps-beforeOps)
	}
	if afterBytes != beforeBytes+float64(1024*1024) {
		t.Errorf("expected bytes counter to increase by %d, got delta=%f", 1024*1024, afterBytes-beforeBytes)
	}
}

func TestRecordBackupFailure(t *testing.T) {
	// Set up: start an operation first
	RecordBackupStart("restore")

	beforeOps := getCounterValue(BackupOperationsTotal.WithLabelValues("restore", "failure", "live"))

	RecordBackupFailure("restore", "live", 5.0)

	afterOps := getCounterValue(BackupOperationsTotal.WithLabelValues("restore", "failure", "live"))

	if afterOps != beforeOps+1 {
		t.Errorf("expected failure counter to increment by 1, got delta=%f", afterOps-beforeOps)
	}
}

func TestRecordBackupCompleteZeroBytes(t *testing.T) {
	RecordBackupStart("backup")
	beforeBytes := getCounterValue(BackupBytesTotal.WithLabelValues("backup"))

	RecordBackupComplete("backup", "live", 10.0, 0)

	afterBytes := getCounterValue(BackupBytesTotal.WithLabelValues("backup"))

	if afterBytes != beforeBytes {
		t.Errorf("expected bytes counter unchanged for zero bytes, got delta=%f", afterBytes-beforeBytes)
	}
}

func TestRecordLastSuccessfulBackup(t *testing.T) {
	RecordLastSuccessfulBackup("test-ns", "test-pvc")

	val := getGaugeValue(BackupLastSuccessfulTimestamp.WithLabelValues("test-ns", "test-pvc"))
	if val <= 0 {
		t.Errorf("expected timestamp > 0, got %f", val)
	}
}

func TestRecordStandbyPVCCreatedAndDeleted(t *testing.T) {
	before := getGaugeValue(BackupStandbyPVCCount.WithLabelValues("test-ns", "created"))

	RecordStandbyPVCCreated("test-ns")
	afterCreate := getGaugeValue(BackupStandbyPVCCount.WithLabelValues("test-ns", "created"))
	if afterCreate != before+1 {
		t.Errorf("expected standby PVC count to increment, got delta=%f", afterCreate-before)
	}

	RecordStandbyPVCDeleted("test-ns")
	afterDelete := getGaugeValue(BackupStandbyPVCCount.WithLabelValues("test-ns", "created"))
	if afterDelete != before {
		t.Errorf("expected standby PVC count to return to original, got delta=%f", afterDelete-before)
	}
}

func TestRecordRepositoryStats(t *testing.T) {
	RecordRepositoryStats("test-repo", 5*1024*1024*1024, 42)

	sizeVal := getGaugeValue(BackupRepositorySizeBytes.WithLabelValues("test-repo"))
	if sizeVal != float64(5*1024*1024*1024) {
		t.Errorf("expected repo size %d, got %f", 5*1024*1024*1024, sizeVal)
	}

	countVal := getGaugeValue(BackupRepositorySnapshotCount.WithLabelValues("test-repo"))
	if countVal != 42 {
		t.Errorf("expected snapshot count 42, got %f", countVal)
	}
}
