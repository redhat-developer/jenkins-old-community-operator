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

package controllers

import (
	"context"
	"fmt"
	"github.com/jenkinsci/kubernetes-operator/pkg/notifications/reason"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/jenkinsci/kubernetes-operator/api/v1alpha2"
	jenkinsv1alpha2 "github.com/jenkinsci/kubernetes-operator/api/v1alpha2"
	jenkinsclient "github.com/jenkinsci/kubernetes-operator/pkg/client"
	"github.com/jenkinsci/kubernetes-operator/pkg/configuration"
	"github.com/jenkinsci/kubernetes-operator/pkg/notifications/event"
	"github.com/jenkinsci/kubernetes-operator/pkg/notifications/reason"
)

// JenkinsReconciler reconciles a Jenkins object
type JenkinsReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	jenkinsAPIConnectionSettings jenkinsclient.JenkinsAPIConnectionSettings
	clientSet                    kubernetes.Clientset
	restConfig                   rest.Config
	notificationEvents           *chan event.Event
}

// +kubebuilder:rbac:groups=jenkins.jenkins.io,resources=jenkins,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=jenkins.jenkins.io,resources=jenkins/status,verbs=get;update;patch

func (r *JenkinsReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	_ = r.Log.WithValues("jenkins", req.NamespacedName)

	// your logic here

	return ctrl.Result{}, nil
}

func (r *JenkinsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&jenkinsv1alpha2.Jenkins{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}


func (r *JenkinsReconciler) newReconcilierConfiguration(jenkins *v1alpha2.Jenkins) configuration.Configuration {
	config := configuration.Configuration{
		Client:                       r.Client,
		ClientSet:                    r.clientSet,
		RestConfig:                   r.restConfig,
		JenkinsAPIConnectionSettings: r.jenkinsAPIConnectionSettings,
		Notifications:                r.notificationEvents,
		Jenkins:                      jenkins,
		Scheme:                       r.Scheme,
	}
	return config
}

// NewReconciler returns a newReconcilierConfiguration reconcile.Reconciler.
func NewReconciler(mgr manager.Manager, jenkinsAPIConnectionSettings jenkinsclient.JenkinsAPIConnectionSettings, clientSet kubernetes.Clientset, config rest.Config, notificationEvents *chan event.Event) reconcile.Reconciler {
	return &JenkinsReconciler{
		client:                       mgr.GetClient(),
		scheme:                       mgr.GetScheme(),
		clientSet:                    clientSet,
		jenkinsAPIConnectionSettings: jenkinsAPIConnectionSettings,
		restConfig:                   config,
		notificationEvents:           notificationEvents,
	}
}

func (r *JenkinsReconciler) sendNewReconcileLoopFailedNotification(jenkins *v1alpha2.Jenkins, reconcileFailLimit uint64, err error) {
	*r.notificationEvents <- event.Event{
		Jenkins: *jenkins,
		Phase:   event.PhaseBase,
		Level:   v1alpha2.NotificationLevelWarning,
		Reason: reason.NewReconcileLoopFailed(
			reason.OperatorSource,
			[]string{fmt.Sprintf("Reconcile loop failed %d times with the same error, giving up: %s", reconcileFailLimit, err)},
		),
	}
}

func (r *JenkinsReconciler) sendNewBaseConfigurationCompleteNotification(jenkins *v1alpha2.Jenkins, message string) {
	*r.notificationEvents <- event.Event{
		Jenkins: *jenkins,
		Phase:   event.PhaseBase,
		Level:   v1alpha2.NotificationLevelInfo,
		Reason:  reason.NewBaseConfigurationComplete(reason.OperatorSource, []string{message}),
	}
}

func (r *JenkinsReconciler) sendNewGroovyScriptExecutionFailedNotification(jenkins *v1alpha2.Jenkins, groovyErr *jenkinsclient.GroovyScriptExecutionFailed) {
	*r.notificationEvents <- event.Event{
		Jenkins: *jenkins,
		Phase:   event.PhaseBase,
		Level:   v1alpha2.NotificationLevelWarning,
		Reason: reason.NewGroovyScriptExecutionFailed(
			reason.OperatorSource,
			[]string{fmt.Sprintf("%s Source '%s' Name '%s' groovy script execution failed", groovyErr.ConfigurationType, groovyErr.Source, groovyErr.Name)},
			[]string{fmt.Sprintf("%s Source '%s' Name '%s' groovy script execution failed, logs: %+v", groovyErr.ConfigurationType, groovyErr.Source, groovyErr.Name, groovyErr.Logs)}...,
		),
	}
}
