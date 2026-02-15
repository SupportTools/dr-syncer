package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/supporttools/dr-syncer/pkg/config"
	"github.com/supporttools/dr-syncer/pkg/controller/backup"
	"github.com/supporttools/dr-syncer/pkg/controller/remotecluster"
	"github.com/supporttools/dr-syncer/pkg/logging"
	"github.com/supporttools/dr-syncer/pkg/version"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	corev1 "k8s.io/api/core/v1"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/controllers"
	"github.com/supporttools/dr-syncer/pkg/controllers/syncer"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(drv1alpha1.AddToScheme(scheme))
}

func main() {
	// Version flag
	showVersion := flag.Bool("version", false, "Display version information")
	showVersionJSON := flag.Bool("version-json", false, "Display version information in JSON format")

	// Load configuration from environment variables
	config.LoadConfiguration()

	// Initialize logging
	log := logging.SetupLogging()

	// Set up controller-runtime logging to use our logger
	logging.SetupControllerRuntimeLogging(log)

	// Log startup information
	log.Info("starting DR Syncer controller")

	// Allow command line flags to override environment variables
	flag.StringVar(&config.CFG.MetricsAddr, "metrics-bind-address", config.CFG.MetricsAddr, "The address the metric endpoint binds to.")
	flag.StringVar(&config.CFG.ProbeAddr, "health-probe-bind-address", config.CFG.ProbeAddr, "The address the probe endpoint binds to.")
	flag.BoolVar(&config.CFG.EnableLeaderElection, "leader-elect", config.CFG.EnableLeaderElection,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	flag.Parse()

	// Handle version flags
	if *showVersion {
		fmt.Println(version.GetVersionString())
		os.Exit(0)
	}
	if *showVersionJSON {
		fmt.Println(version.GetVersionJSON())
		os.Exit(0)
	}

	// Log configuration settings
	log.Info("configuration loaded")

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: config.CFG.MetricsAddr,
		},
		HealthProbeBindAddress: config.CFG.ProbeAddr,
		LeaderElection:         config.CFG.EnableLeaderElection,
		LeaderElectionID:       config.CFG.LeaderElectionID,
	})
	if err != nil {
		log.Error("unable to start manager")
		os.Exit(1)
	}

	log.Info("setting up controllers")

	// Set up RemoteCluster controller
	if err = (&controllers.RemoteClusterReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		log.Error("unable to create RemoteCluster controller")
		os.Exit(1)
	}
	log.Info("configured RemoteCluster controller")

	// Set up NamespaceMapping controller with backup sync support
	backupSyncFunc := newBackupSyncFunc(mgr.GetClient())
	standbyCleanupFunc := newStandbyPVCCleanupFunc()
	if err = (&controllers.NamespaceMappingReconciler{
		Client:                mgr.GetClient(),
		Scheme:                mgr.GetScheme(),
		BackupSyncFunc:        backupSyncFunc,
		StandbyPVCCleanupFunc: standbyCleanupFunc,
	}).SetupWithManager(mgr); err != nil {
		log.Error("unable to create NamespaceMapping controller")
		os.Exit(1)
	}
	log.Info("configured NamespaceMapping controller")

	// Set up ClusterMapping controller
	if err = (&controllers.ClusterMappingReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		log.Error("unable to create ClusterMapping controller")
		os.Exit(1)
	}
	log.Info("configured ClusterMapping controller")

	// Set up BackupRepository controller
	kopiaClient := backup.NewKopiaRepositoryClient(mgr.GetClient())
	if err = (&controllers.BackupRepositoryReconciler{
		Client:     mgr.GetClient(),
		Scheme:     mgr.GetScheme(),
		RepoClient: kopiaClient,
	}).SetupWithManager(mgr); err != nil {
		log.Error("unable to create BackupRepository controller")
		os.Exit(1)
	}
	log.Info("configured BackupRepository controller")

	// Set up health checks
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Error("unable to set up health check")
		os.Exit(1)
	}
	log.Info("configured health check endpoint")

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.Error("unable to set up ready check")
		os.Exit(1)
	}
	log.Info("configured readiness check endpoint")

	log.Info("performing initial agent sync")
	if err := remotecluster.SyncAllAgents(context.Background(), mgr.GetClient()); err != nil {
		log.Warnf("initial agent sync encountered issues: %v", err)
		// Continue anyway, as normal reconciliation will retry
	}

	log.Info("starting manager")

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Error("problem running manager")
		os.Exit(1)
	}
}

// newStandbyPVCCleanupFunc creates a StandbyPVCCleanupFunc that wraps backup.StandbyPVCManager.
// This adapter lives in main.go to break the import cycle between controllers and backup packages.
func newStandbyPVCCleanupFunc() controllers.StandbyPVCCleanupFunc {
	return func(ctx context.Context, destConfig *rest.Config, scheme *runtime.Scheme,
		mapping *drv1alpha1.NamespaceMapping) error {

		destRuntimeClient, err := client.New(destConfig, client.Options{Scheme: scheme})
		if err != nil {
			return fmt.Errorf("create destination runtime client: %w", err)
		}

		destNamespace := mapping.Spec.DestinationNamespace
		if destNamespace == "" {
			destNamespace = mapping.Spec.SourceNamespace
		}

		var standbyConfig *drv1alpha1.StandbyPVCConfig
		if mapping.Spec.PVCConfig != nil &&
			mapping.Spec.PVCConfig.DataSyncConfig != nil &&
			mapping.Spec.PVCConfig.DataSyncConfig.BackupConfig != nil {
			standbyConfig = mapping.Spec.PVCConfig.DataSyncConfig.BackupConfig.StandbyPVCConfig
		}

		standbyMgr := backup.NewStandbyPVCManager(
			destRuntimeClient,
			standbyConfig,
			mapping.Spec.PVCConfig,
			mapping.Name,
		)

		return standbyMgr.CleanupStandbyPVCs(ctx, destNamespace)
	}
}

// newBackupSyncFunc creates a BackupPVCSyncFunc that wraps backup.VolumeBackupSyncer.
// This adapter lives in main.go to break the import cycle between syncer and backup packages.
func newBackupSyncFunc(ctrlClient client.Client) syncer.BackupPVCSyncFunc {
	return func(ctx context.Context, mapping *drv1alpha1.NamespaceMapping,
		pvcs []corev1.PersistentVolumeClaim, backupConfig *drv1alpha1.BackupConfig) []syncer.BackupPVCSyncResult {

		vbs := backup.NewVolumeBackupSyncer(ctrlClient, ctrlClient)
		results := vbs.SyncPVCsWithBackup(ctx, mapping, pvcs, backupConfig)

		// Convert backup.PVCSyncResult to syncer.BackupPVCSyncResult
		syncResults := make([]syncer.BackupPVCSyncResult, len(results))
		for i, r := range results {
			syncResults[i] = syncer.BackupPVCSyncResult{
				PVCName:    r.PVCName,
				SnapshotID: r.SnapshotID,
				Err:        r.Err,
			}
		}
		return syncResults
	}
}
