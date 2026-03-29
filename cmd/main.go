package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"
	"strings"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	pangolinv1alpha1 "github.com/home-operations/pangolin-operator/api/v1alpha1"
	"github.com/home-operations/pangolin-operator/internal/controller/newtsite"
	"github.com/home-operations/pangolin-operator/internal/controller/privateresource"
	"github.com/home-operations/pangolin-operator/internal/controller/publicresource"
	"github.com/home-operations/pangolin-operator/internal/pangolin"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	// Register only the API groups we actually use instead of the entire
	// client-go scheme. This prevents controller-runtime from creating
	// informer caches for every built-in Kubernetes type.
	utilruntime.Must(appsv1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(pangolinv1alpha1.AddToScheme(scheme))
	utilruntime.Must(gatewayv1.Install(scheme))
	// +kubebuilder:scaffold:scheme
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		setupLog.Error(fmt.Errorf("required environment variable %q is not set", key), "missing configuration")
		os.Exit(1)
	}
	return v
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var enablePprof bool
	var pprofAddr string
	var logLevel string
	var tlsOpts []func(*tls.Config)

	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics server")
	flag.BoolVar(&enablePprof, "enable-pprof", false,
		"If set, a pprof endpoint will be exposed on the pprof-bind-address for runtime profiling")
	flag.StringVar(&pprofAddr, "pprof-bind-address", ":6060",
		"The address the pprof endpoint binds to. Only used when --enable-pprof is set.")
	flag.StringVar(&logLevel, "log-level", "info",
		"Log level for the controller (debug, info)")

	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	switch strings.ToLower(logLevel) {
	case "debug":
		opts.Development = true
	default:
		opts.Development = false
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if enablePprof {
		mux := http.NewServeMux()
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

		srv := &http.Server{Addr: pprofAddr, Handler: mux}
		setupLog.Info("starting pprof server", "address", pprofAddr)
		go func() {
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				setupLog.Error(err, "pprof server failed")
			}
		}()
	}

	newtEndpoint := mustEnv("PANGOLIN_ENDPOINT")

	pc := pangolin.NewClient(pangolin.Credentials{
		Endpoint: mustEnv("PANGOLIN_API_URL"),
		APIKey:   mustEnv("PANGOLIN_API_KEY"),
		OrgID:    mustEnv("PANGOLIN_ORG_ID"),
	})

	// Disable HTTP/2 by default to avoid CVE-2023-44487 and CVE-2023-39325.
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}
	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}
	if secureMetrics {
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:  scheme,
		Metrics: metricsServerOptions,
		Cache: cache.Options{
			// Only cache the specific types our controllers use. Without this
			// restriction controller-runtime creates informers for every type
			// registered in the scheme, wasting significant memory.
			ByObject: map[client.Object]cache.ByObject{
				&appsv1.Deployment{}:                {},
				&corev1.Secret{}:                    {},
				&corev1.ServiceAccount{}:            {},
				&corev1.Service{}:                   {},
				&gatewayv1.HTTPRoute{}:              {},
				&pangolinv1alpha1.NewtSite{}:        {},
				&pangolinv1alpha1.PublicResource{}:  {},
				&pangolinv1alpha1.PrivateResource{}: {},
			},
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "pangolin-operator.home-operations.com",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	setupLog.Info("Setting up controllers")

	if err := (&newtsite.Reconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		PangolinClient: pc,
		NewtEndpoint:   newtEndpoint,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "NewtSite")
		os.Exit(1)
	}

	if err := (&publicresource.Reconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		PangolinClient: pc,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PublicResource")
		os.Exit(1)
	}

	if err := (&privateresource.Reconciler{
		Client:         mgr.GetClient(),
		Scheme:         mgr.GetScheme(),
		PangolinClient: pc,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "PrivateResource")
		os.Exit(1)
	}

	// +kubebuilder:scaffold:builder

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
