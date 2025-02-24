package main

import (
	"flag"
	"fmt"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/supporttools/dr-syncer/pkg/config"
	"github.com/supporttools/dr-syncer/pkg/logging"
	"github.com/supporttools/dr-syncer/pkg/version"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	drv1alpha1 "github.com/supporttools/dr-syncer/api/v1alpha1"
	"github.com/supporttools/dr-syncer/pkg/controllers"
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

	// Set up Replication controller
	if err = (&controllers.ReplicationReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		log.Error("unable to create Replication controller")
		os.Exit(1)
	}
	log.Info("configured Replication controller")

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

	log.Info("starting manager")

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Error("problem running manager")
		os.Exit(1)
	}
}
