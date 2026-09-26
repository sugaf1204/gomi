package main

import (
	"flag"
	"os"

	infrav1 "github.com/sugaf1204/gomi/providers/cluster-api/api/v1alpha1"
	"github.com/sugaf1204/gomi/providers/cluster-api/internal/controllers"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func main() {
	var namespace, metrics, health string
	var leader bool
	flag.StringVar(&namespace, "namespace", "", "Namespace to watch (empty means all)")
	flag.StringVar(&metrics, "metrics-bind-address", ":8080", "Metrics address")
	flag.StringVar(&health, "health-probe-bind-address", ":8081", "Health address")
	flag.BoolVar(&leader, "leader-elect", true, "Enable leader election")
	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	scheme := runtime.NewScheme()
	must(corev1.AddToScheme(scheme))
	must(clusterv1.AddToScheme(scheme))
	must(infrav1.AddToScheme(scheme))
	options := ctrl.Options{Scheme: scheme, Metrics: metricsserver.Options{BindAddress: metrics}, HealthProbeBindAddress: health, LeaderElection: leader, LeaderElectionID: "gomi.infrastructure.cluster.x-k8s.io"}
	if namespace != "" {
		options.Cache = cache.Options{DefaultNamespaces: map[string]cache.Config{namespace: {}}}
	}
	manager, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	must(err)
	must((&controllers.ClusterReconciler{Client: manager.GetClient()}).SetupWithManager(manager))
	must((&controllers.MachineReconciler{Client: manager.GetClient()}).SetupWithManager(manager))
	must(manager.AddHealthzCheck("healthz", healthz.Ping))
	must(manager.AddReadyzCheck("readyz", healthz.Ping))
	must(manager.Start(ctrl.SetupSignalHandler()))
}
func must(err error) {
	if err != nil {
		ctrl.Log.Error(err, "manager failed")
		os.Exit(1)
	}
}
