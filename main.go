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
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"os"
	ctrl "sigs.k8s.io/controller-runtime"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	jenkinsv1alpha2 "github.com/jenkinsci/kubernetes-operator/api/v1alpha2"
	"github.com/jenkinsci/kubernetes-operator/controllers"
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
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	ctrl.SetLogger(kzap.New(kzap.UseDevMode(true)))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "9cf053ac.jenkins.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controllers.JenkinsReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("Jenkins"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Jenkins")
		os.Exit(1)
	}
	if err = (&controllers.JenkinsImageReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("JenkinsImage"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "JenkinsImage")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
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
