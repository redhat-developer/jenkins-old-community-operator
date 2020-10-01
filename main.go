/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"fmt"
	"github.com/jenkinsci/kubernetes-operator/pkg/client"
	"github.com/jenkinsci/kubernetes-operator/pkg/configuration/base/resources"
	"github.com/jenkinsci/kubernetes-operator/pkg/constants"
	"github.com/jenkinsci/kubernetes-operator/pkg/event"
	"github.com/jenkinsci/kubernetes-operator/pkg/notifications"
	e "github.com/jenkinsci/kubernetes-operator/pkg/notifications/event"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/pkg/errors"
	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"os"
	currentruntime "runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	jenkinsv1alpha2 "github.com/jenkinsci/kubernetes-operator/api/v1alpha2"
	"github.com/jenkinsci/kubernetes-operator/controllers"
	"github.com/jenkinsci/kubernetes-operator/version"
	sdkVersion "github.com/operator-framework/operator-sdk/version"

	kzap "sigs.k8s.io/controller-runtime/pkg/log/zap"

	"k8s.io/apimachinery/pkg/runtime/schema"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(jenkinsv1alpha2.AddToScheme(scheme))
	utilruntime.Must(routev1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	parsePglags(metricsAddr, enableLeaderElection)
	hostname := pflag.String("jenkins-api-hostname", "", "Hostname or IP of Jenkins API. It can be service name, node IP or localhost.")
	port := pflag.Int("jenkins-api-port", 0, "The port on which Jenkins API is running. Note: If you want to use nodePort don't set this setting and --jenkins-api-use-nodeport must be true.")
	useNodePort := pflag.Bool("jenkins-api-use-nodeport", false, "Connect to Jenkins API using the service nodePort instead of service port. If you want to set this as true - don't set --jenkins-api-port.")
	debug := pflag.Bool("debug", false, "Set log level to debug")

	mgr := initManager(metricsAddr, enableLeaderElection)

	// get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		fatal(errors.Wrap(err, "failed to get config"), *debug)
	}
	setupLog.Info("Registering Components.")

	// setup Scheme for all resources
	//if err := apis.AddToScheme(mgr.GetScheme()); err != nil {
	//	fatal(errors.Wrap(err, "failed to setup scheme"), *debug)
	//}

	// setup events
	events, err := event.New(cfg, constants.OperatorName)
	if err != nil {
		fatal(errors.Wrap(err, "failed to create manager"), *debug)
	}

	clientSet, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		fatal(errors.Wrap(err, "failed to create Kubernetes client set"), *debug)
	}

	if resources.IsRouteAPIAvailable(clientSet) {
		setupLog.Info("Route API found: Route creation will be performed")
	}

	if resources.IsImageRegistryAvailable(clientSet) {
		setupLog.Info("Internal Image Registry found: It is very likely that we are running on OpenShift")
		setupLog.Info("If JenkinsImages are built without specified destination, they will be pushed into it.")
	}

	c := make(chan e.Event)
	go notifications.Listen(c, events, mgr.GetClient())

	// validate jenkins API connection
	jenkinsAPIConnectionSettings := client.JenkinsAPIConnectionSettings{Hostname: *hostname, Port: *port, UseNodePort: *useNodePort}
	if err := jenkinsAPIConnectionSettings.Validate(); err != nil {
		fatal(errors.Wrap(err, "invalid command line parameters"), *debug)
	}

	// setup Jenkins controller
	setupJenkinsRenconciler(mgr)
	setupJenkinsImageRenconciler(mgr)
	// start the Cmd
	setupLog.Info("Starting the Cmd.")
	runMananger(mgr)
	// +kubebuilder:scaffold:builder
}

func parsePglags(metricsAddr string, enableLeaderElection bool) {
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()
	ctrl.SetLogger(kzap.New(kzap.UseDevMode(true)))
}

func initManager(metricsAddr string, enableLeaderElection bool) manager.Manager {
	printInfo()
	mgr, err := startManager(metricsAddr, enableLeaderElection)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}
	return mgr
}

func runMananger(mgr manager.Manager) {
	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
	setupLog.Info("manager started")
}

func startManager(metricsAddr string, enableLeaderElection bool) (manager.Manager, error) {
	options := ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "9cf053ac.jenkins.io",
	}
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}
	return mgr, err
}

func setupJenkinsRenconciler(mgr manager.Manager) {
	if err := newJenkinsReconciler(mgr).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Jenkins")
		os.Exit(1)
	}
}

func newJenkinsReconciler(mgr manager.Manager) *controllers.JenkinsReconciler {
	return &controllers.JenkinsReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("Jenkins"),
		Scheme: mgr.GetScheme(),
	}
}

func setupJenkinsImageRenconciler(mgr manager.Manager) {
	if err := newJenkinsImageRenconciler(mgr).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Jenkins")
		os.Exit(1)
	}
}

func newJenkinsImageRenconciler(mgr manager.Manager) *controllers.JenkinsImageReconciler {
	return &controllers.JenkinsImageReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("JenkinsImage"),
		Scheme: mgr.GetScheme(),
	}
}

func filterGKVsFromAddToScheme(gvks []schema.GroupVersionKind) []schema.GroupVersionKind {
	// We use gkvFilters to filter from the existing GKVs defined in the used
	// runtime.Schema for the operator. The reason for that is that
	// kube-metrics tries to list all of the defined Kinds in the schemas
	// that are passed, including Kinds that the operator doesn't use and
	// thus the role used the operator doesn't have them set and we don't want
	// to set as they are not used by the operator.
	// For the fields that the filters have we have defined the value '*' to
	// specify any will be a match (accepted)
	matchAnyValue := "*"
	gvkFilters := []schema.GroupVersionKind{
		// Kubernetes Resources
		{Kind: "PersistentVolumeClaim", Version: matchAnyValue},
		{Kind: "ServiceAccount", Version: matchAnyValue},
		{Kind: "Secret", Version: matchAnyValue},
		{Kind: "Pod", Version: matchAnyValue},
		{Kind: "ConfigMap", Version: matchAnyValue},
		{Kind: "Service", Version: matchAnyValue},
		{Group: "apps", Kind: "Deployment", Version: matchAnyValue},
		// Openshift Resources
		{Group: "route.openshift.io", Kind: "Route", Version: matchAnyValue},
		{Group: "image.openshift.io", Kind: "ImageStream", Version: matchAnyValue},
		// Custom Resources
		{Group: "jenkins.io", Kind: "Jenkins", Version: matchAnyValue},
		{Group: "jenkins.io", Kind: "JenkinsImage", Version: matchAnyValue},
		{Group: "jenkins.io", Kind: "Casc", Version: matchAnyValue},
	}

	ownGVKs := []schema.GroupVersionKind{}
	for _, gvk := range gvks {
		for _, gvkFilter := range gvkFilters {
			match := true
			if gvkFilter.Kind == matchAnyValue && gvkFilter.Group == matchAnyValue && gvkFilter.Version == matchAnyValue {
				setupLog.V(1).Info("gvkFilter should at least have one of its fields defined. Skipping...")
				match = false
			} else {
				if gvkFilter.Kind != matchAnyValue && gvkFilter.Kind != gvk.Kind {
					match = false
				}
				if gvkFilter.Group != matchAnyValue && gvkFilter.Group != gvk.Group {
					match = false
				}
				if gvkFilter.Version != matchAnyValue && gvkFilter.Version != gvk.Version {
					match = false
				}
			}
			if match {
				ownGVKs = append(ownGVKs, gvk)
			}
		}
	}

	return ownGVKs
}

func fatal(err error, debug bool) {
	if debug {
		setupLog.Error(nil, fmt.Sprintf("%+v", err))
	} else {
		setupLog.Error(nil, fmt.Sprintf("%s", err))
	}
	os.Exit(-1)
}

func printInfo() {
	setupLog.Info(fmt.Sprintf("Version: %s", version.Version))
	setupLog.Info(fmt.Sprintf("Git commit: %s", version.GitCommit))
	setupLog.Info(fmt.Sprintf("Go Version: %s", currentruntime.Version()))
	setupLog.Info(fmt.Sprintf("Go OS/Arch: %s/%s", currentruntime.GOOS, currentruntime.GOARCH))
	setupLog.Info(fmt.Sprintf("operator-sdk Version: %v", sdkVersion.Version))
}
