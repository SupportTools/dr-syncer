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

drv1alpha1 "github.com/supporttools/dr-syncer/pkg/api/v1alpha1"
"github.com/supporttools/dr-syncer/pkg/controllers"
)

var (
scheme   = runtime.NewScheme()
setupLog = ctrl.Log.WithName("setup")
)

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
logging.SetupLogging()
ctrl.SetLogger(logging.NewLogrusLogr())

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
setupLog.Error(err, "unable to start manager")
os.Exit(1)
}

if err = (&controllers.RemoteClusterReconciler{
Client: mgr.GetClient(),
Scheme: mgr.GetScheme(),
}).SetupWithManager(mgr); err != nil {
setupLog.Error(err, "unable to create controller", "controller", "RemoteCluster")
os.Exit(1)
}

if err = (&controllers.ReplicationReconciler{
Client: mgr.GetClient(),
Scheme: mgr.GetScheme(),
}).SetupWithManager(mgr); err != nil {
setupLog.Error(err, "unable to create controller", "controller", "Replication")
os.Exit(1)
}

if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
setupLog.Error(err, "unable to set up health check")
os.Exit(1)
}
if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
setupLog.Error(err, "unable to set up ready check")
os.Exit(1)
}

setupLog.Info("starting manager")
if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
setupLog.Error(err, "problem running manager")
os.Exit(1)
}
}
