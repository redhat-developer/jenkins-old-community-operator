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
	"github.com/jenkinsci/kubernetes-operator/pkg/configuration/base"
	"github.com/jenkinsci/kubernetes-operator/pkg/configuration/base/resources"
	"github.com/jenkinsci/kubernetes-operator/pkg/constants"
	"github.com/jenkinsci/kubernetes-operator/pkg/plugins"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"math/rand"
	"reflect"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"strings"

	"github.com/jenkinsci/kubernetes-operator/pkg/log"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"time"

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

const (
	APIVersion             = "core/v1"
	DeploymentKind         = "Deployment"
	SecretKind             = "Secret"
	ConfigMapKind          = "ConfigMap"
	containerProbeURI      = "login"
	containerProbePortName = "http"
)

// JenkinsReconciler reconciles a Jenkins object
type JenkinsReconciler struct {
	client.Client
	Log                          logr.Logger
	Scheme                       *runtime.Scheme
	jenkinsAPIConnectionSettings jenkinsclient.JenkinsAPIConnectionSettings
	clientSet                    kubernetes.Clientset
	restConfig                   rest.Config
	notificationEvents           *chan event.Event
}

type reconcileError struct {
	err     error
	counter uint64
}

var reconcileErrors = map[string]reconcileError{}

func (r *JenkinsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&jenkinsv1alpha2.Jenkins{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

// +kubebuilder:rbac:groups=jenkins.jenkins.io,resources=jenkins,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=jenkins.jenkins.io,resources=jenkins/status,verbs=get;update;patch

func (r *JenkinsReconciler) Reconcile(request ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	_ = r.Log.WithValues("jenkins", request.NamespacedName)

	reconcileFailLimit := uint64(10)
	logger := r.Log
	logger.V(log.VDebug).Info(fmt.Sprintf("Reconciling Jenkins: %s", request.Name))
	result, jenkins, err := r.reconcile(request)
	if err != nil && apierrors.IsConflict(err) {
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		lastErrors, found := reconcileErrors[request.Name]
		if found {
			if err.Error() == lastErrors.err.Error() {
				lastErrors.counter++
			} else {
				lastErrors.counter = 1
				lastErrors.err = err
			}
		} else {
			lastErrors = reconcileError{
				err:     err,
				counter: 1,
			}
		}
		reconcileErrors[request.Name] = lastErrors
		if lastErrors.counter >= reconcileFailLimit {
			logger.V(log.VWarn).Info(fmt.Sprintf("Reconcile loop failed %d times with the same error, giving up: %+v", reconcileFailLimit, err))
			r.sendNewReconcileLoopFailedNotification(jenkins, reconcileFailLimit, err)
			return ctrl.Result{}, nil
		}

		if log.Debug {
			logger.V(log.VWarn).Info(fmt.Sprintf("Reconcile loop failed: %+v", err))
		} else if err.Error() != fmt.Sprintf("Operation cannot be fulfilled on jenkins.jenkins.io \"%s\": the object has been modified; please apply your changes to the latest version and try again", request.Name) {
			logger.V(log.VWarn).Info(fmt.Sprintf("Reconcile loop failed: %s", err))
		}

		if groovyErr, ok := err.(*jenkinsclient.GroovyScriptExecutionFailed); ok {
			r.sendNewGroovyScriptExecutionFailedNotification(jenkins, groovyErr)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{Requeue: true}, nil
	}
	if result.Requeue && result.RequeueAfter == 0 {
		result.RequeueAfter = time.Duration(rand.Intn(10)) * time.Millisecond
	}
	return ctrl.Result{}, nil
}

func (r *JenkinsReconciler) reconcile(request ctrl.Request) (ctrl.Result, *v1alpha2.Jenkins, error) {
	logger := r.Log.WithValues("cr", request.Name)
	// Fetch the Jenkins instance
	jenkins := &v1alpha2.Jenkins{}
	var err error
	err = r.Client.Get(context.TODO(), request.NamespacedName, jenkins)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.V(log.VWarn).Info(fmt.Sprintf("Object not found: %s: %+v", request, jenkins))
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil, nil
		}
		// Error reading the object - requeue the request.
		logger.V(log.VWarn).Info(fmt.Sprintf("Error reading object not found: %s: %+v", request, jenkins))
		return reconcile.Result{}, nil, errors.WithStack(err)
	}
	var requeue bool
	requeue, err = r.setDefaults(jenkins)
	if err != nil {
		return reconcile.Result{}, jenkins, err
	}
	if requeue {
		return reconcile.Result{Requeue: true}, jenkins, nil
	}

	requeue, err = r.handleDeprecatedData(jenkins)
	if err != nil {
		return reconcile.Result{}, jenkins, err
	}
	if requeue {
		return reconcile.Result{Requeue: true}, jenkins, nil
	}

	config := r.newReconcilierConfiguration(jenkins)
	// Reconcile base configuration
	logger.V(log.VDebug).Info("Starting base configuration reconciliation for validation")
	baseConfiguration := base.New(config, r.jenkinsAPIConnectionSettings)
	var baseConfigurationValidationMessages []string
	baseConfigurationValidationMessages, err = baseConfiguration.Validate(jenkins)
	if err != nil {
		logger.V(log.VDebug).Info(fmt.Sprintf("Error while trying to validate base configuration %s", err))
		return reconcile.Result{}, jenkins, err
	}
	if len(baseConfigurationValidationMessages) > 0 {
		message := "Validation of base configuration failed, please correct Jenkins CR."
		r.sendNewBaseConfigurationFailedNotification(jenkins, message, baseConfigurationValidationMessages)
		logger.V(log.VWarn).Info(message)
		for _, msg := range baseConfigurationValidationMessages {
			logger.V(log.VWarn).Info(msg)
		}
		return reconcile.Result{}, jenkins, nil // don't requeue
	}
	logger.V(log.VDebug).Info("Base configuration validation finished: No errors on validation messages")
	logger.V(log.VDebug).Info("Starting base configuration reconciliation...")
	_, jenkinsClient, err := baseConfiguration.Reconcile()
	if err != nil {
		if r.isJenkinsPodTerminating(err) {
			logger.Info(fmt.Sprintf("Jenkins Pod in Terminating state with DeletionTimestamp set detected. Changing Jenkins Phase to %s", constants.JenkinsStatusReinitializing))
			jenkins.Status.Phase = constants.JenkinsStatusReinitializing
			jenkins.Status.BaseConfigurationCompletedTime = nil
			// update Jenkins CR Status from Completed to Reinitializing
			err = r.Client.Update(context.TODO(), jenkins)
			if err != nil {
				return reconcile.Result{}, jenkins, errors.WithStack(err)
			}
			logger.Info("Base configuration reinitialized, jenkins pod restarted")
			return reconcile.Result{Requeue: true}, jenkins, err
		}
		logger.V(log.VDebug).Info(fmt.Sprintf("Base configuration reconciliation failed with error, requeuing:  %s ", err))
		//FIXME What we do not requeue ?
		return reconcile.Result{}, jenkins, err
	}
	logger.V(log.VDebug).Info("Base configuration reconciliation successful.")
	//if result.Requeue {
	//	return result, jenkins, nil
	//}
	if jenkinsClient == nil {
		logger.V(log.VDebug).Info("Base configuration reconciliation succeeded but returned a nil jenkinsClient. Cannot continue.")
		return reconcile.Result{}, jenkins, nil
	}
	logger.V(log.VDebug).Info(fmt.Sprintf("Base configuration reconcialiation finished successfully: jenkinsClient %+v created", jenkinsClient))
	if jenkins.Status.BaseConfigurationCompletedTime == nil {
		now := metav1.Now()
		jenkins.Status.Phase = constants.JenkinsStatusCompleted
		jenkins.Status.BaseConfigurationCompletedTime = &now
		err = r.Client.Update(context.TODO(), jenkins)
		if err != nil {
			return reconcile.Result{}, jenkins, errors.WithStack(err)
		}
		time := jenkins.Status.BaseConfigurationCompletedTime.Sub(jenkins.Status.ProvisionStartTime.Time)
		message := fmt.Sprintf("Base configuration phase is complete, took %s", time)
		r.sendNewBaseConfigurationCompleteNotification(jenkins, message)
		logger.Info(message)
	}

	// Reconcile seedjobs and backups
	/*userConfiguration := user.New(config, jenkinsClient)

	var messages []string
	messages, err = userConfiguration.Validate(jenkins)
	if err != nil {
		return reconcile.Result{}, jenkins, err
	}
	if len(messages) > 0 {
		message := "Validation of user configuration failed, please correct Jenkins CR"
		r.sendNewUserConfigurationFailedNotification(jenkins, message, messages)

		logger.V(log.VWarn).Info(message)
		for _, msg := range messages {
			logger.V(log.VWarn).Info(msg)
		}
		return reconcile.Result{}, jenkins, nil // don't requeue
	}

	// Reconcile seedjobs, backups
	result, err = userConfiguration.ReconcileOthers()
	if err != nil {
		return reconcile.Result{}, jenkins, err
	}
	if result.Requeue {
		return result, jenkins, nil
	}

	if jenkins.Status.UserConfigurationCompletedTime == nil {
		now := metav1.Now()
		jenkins.Status.UserConfigurationCompletedTime = &now
		err = r.client.Update(context.TODO(), jenkins)
		if err != nil {
			return reconcile.Result{}, jenkins, errors.WithStack(err)
		}
		time := jenkins.Status.UserConfigurationCompletedTime.Sub(jenkins.Status.ProvisionStartTime.Time)
		message := fmt.Sprintf("User configuration phase is complete, took %s", time)
		r.sendNewUserConfigurationCompleteNotification(jenkins, message)
		logger.Info(message)
	}*/
	return reconcile.Result{}, jenkins, nil
}

func (r *JenkinsReconciler) sendNewBaseConfigurationFailedNotification(jenkins *v1alpha2.Jenkins, message string, baseMessages []string) {
	*r.notificationEvents <- event.Event{
		Jenkins: *jenkins,
		Phase:   event.PhaseBase,
		Level:   v1alpha2.NotificationLevelWarning,
		Reason:  reason.NewBaseConfigurationFailed(reason.HumanSource, []string{message}, append([]string{message}, baseMessages...)...),
	}
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

func (r *JenkinsReconciler) setDefaults(jenkins *v1alpha2.Jenkins) (requeue bool, err error) {
	changed := false
	logger := r.Log.WithValues("cr", jenkins.Name)

	var jenkinsContainer v1alpha2.Container
	if len(jenkins.Spec.Master.Containers) == 0 {
		changed = true
		jenkinsContainer = v1alpha2.Container{Name: resources.JenkinsMasterContainerName}
	} else {
		if jenkins.Spec.Master.Containers[0].Name != resources.JenkinsMasterContainerName {
			return false, errors.Errorf("first container in spec.master.containers must be Jenkins container with name '%s', please correct CR", resources.JenkinsMasterContainerName)
		}
		jenkinsContainer = jenkins.Spec.Master.Containers[0]
	}

	if len(jenkinsContainer.Image) == 0 {
		jenkinsMasterImage := constants.DefaultJenkinsMasterImage
		changed = true
		if resources.IsRouteAPIAvailable(&r.clientSet) {
			jenkinsMasterImage = constants.DefaultOpenShiftJenkinsMasterImage
		}
		logger.Info("Setting default Jenkins master image: " + jenkinsMasterImage)
		jenkinsContainer.Image = jenkinsMasterImage
		jenkinsContainer.ImagePullPolicy = corev1.PullAlways
	}
	if len(jenkinsContainer.ImagePullPolicy) == 0 {
		logger.Info(fmt.Sprintf("Setting default Jenkins master image pull policy: %s", corev1.PullAlways))
		changed = true
		jenkinsContainer.ImagePullPolicy = corev1.PullAlways
	}

	if jenkinsContainer.ReadinessProbe == nil {
		logger.Info("Setting default Jenkins readinessProbe")
		changed = true
		jenkinsContainer.ReadinessProbe = resources.NewSimpleProbe(containerProbeURI, containerProbePortName, corev1.URISchemeHTTP, 30)
	}
	if jenkinsContainer.LivenessProbe == nil {
		logger.Info("Setting default Jenkins livenessProbe")
		changed = true
		jenkinsContainer.LivenessProbe = resources.NewProbe(containerProbeURI, containerProbePortName, corev1.URISchemeHTTP, 80, 5, 12)
	}
	if len(jenkinsContainer.Command) == 0 && !resources.IsRouteAPIAvailable(&r.clientSet) {
		logger.Info("Setting default Jenkins container command")
		jenkinsContainer.Command = resources.GetJenkinsMasterContainerBaseCommand()
		changed = true
	}
	if isJavaOpsVariableNotSet(jenkinsContainer) {
		logger.Info("Setting default Jenkins container JAVA_OPTS environment variable")
		changed = true
		jenkinsContainer.Env = append(jenkinsContainer.Env, corev1.EnvVar{
			Name:  constants.JavaOpsVariableName,
			Value: "-XX:+UnlockExperimentalVMOptions -XX:+UseCGroupMemoryLimitForHeap -XX:MaxRAMFraction=1 -Djenkins.install.runSetupWizard=false -Djava.awt.headless=true -Dcasc.reload.token=$(POD_NAME)",
		})
	}
	if len(jenkins.Spec.Master.BasePlugins) == 0 {
		logger.Info("Setting default operator plugins")
		changed = true
		jenkins.Spec.Master.BasePlugins = basePlugins()
	}
	if isResourceRequirementsNotSet(jenkinsContainer.Resources) {
		logger.Info("Setting default Jenkins master container resource requirements")
		changed = true
		jenkinsContainer.Resources = resources.NewResourceRequirements("1", "500Mi", "1500m", "3Gi")
	}
	if reflect.DeepEqual(jenkins.Spec.Service, v1alpha2.Service{}) {
		logger.Info("Setting default Jenkins master service")
		changed = true
		var serviceType = corev1.ServiceTypeClusterIP
		if r.jenkinsAPIConnectionSettings.UseNodePort {
			serviceType = corev1.ServiceTypeNodePort
		}
		jenkins.Spec.Service = v1alpha2.Service{
			Type: serviceType,
			Port: constants.DefaultHTTPPortInt32,
		}
	}
	if reflect.DeepEqual(jenkins.Spec.SlaveService, v1alpha2.Service{}) {
		logger.Info("Setting default Jenkins slave service")
		changed = true
		jenkins.Spec.SlaveService = v1alpha2.Service{
			Type: corev1.ServiceTypeClusterIP,
			Port: constants.DefaultJNLPPortInt32,
		}
	}
	if len(jenkins.Spec.Master.Containers) > 1 {
		for i, container := range jenkins.Spec.Master.Containers[1:] {
			if r.setDefaultsForContainer(jenkins, container.Name, i+1) {
				changed = true
			}
		}
	}
	if len(jenkins.Spec.Backup.ContainerName) > 0 && jenkins.Spec.Backup.Interval == 0 {
		logger.Info("Setting default backup interval")
		changed = true
		jenkins.Spec.Backup.Interval = 30
	}

	if len(jenkins.Spec.Master.Containers) == 0 || len(jenkins.Spec.Master.Containers) == 1 {
		jenkins.Spec.Master.Containers = []v1alpha2.Container{jenkinsContainer}
	} else {
		noJenkinsContainers := jenkins.Spec.Master.Containers[1:]
		containers := []v1alpha2.Container{jenkinsContainer}
		containers = append(containers, noJenkinsContainers...)
		jenkins.Spec.Master.Containers = containers
	}

	if reflect.DeepEqual(jenkins.Spec.JenkinsAPISettings, v1alpha2.JenkinsAPISettings{}) {
		logger.Info("Setting default Jenkins API settings")
		changed = true
		jenkins.Spec.JenkinsAPISettings = v1alpha2.JenkinsAPISettings{AuthorizationStrategy: v1alpha2.CreateUserAuthorizationStrategy}
	}

	if jenkins.Spec.JenkinsAPISettings.AuthorizationStrategy == "" {
		logger.Info("Setting default Jenkins API settings authorization strategy")
		changed = true
		jenkins.Spec.JenkinsAPISettings.AuthorizationStrategy = v1alpha2.CreateUserAuthorizationStrategy
	}

	if changed {
		return changed, errors.WithStack(r.Client.Update(context.TODO(), jenkins))
	}
	return changed, nil
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

func isJavaOpsVariableNotSet(container v1alpha2.Container) bool {
	for _, env := range container.Env {
		if env.Name == constants.JavaOpsVariableName {
			return false
		}
	}
	return true
}

func (r *JenkinsReconciler) setDefaultsForContainer(jenkins *v1alpha2.Jenkins, containerName string, containerIndex int) bool {
	changed := false
	logger := r.Log.WithValues("cr", jenkins.Name, "container", containerName)

	if len(jenkins.Spec.Master.Containers[containerIndex].ImagePullPolicy) == 0 {
		logger.Info(fmt.Sprintf("Setting default container image pull policy: %s", corev1.PullAlways))
		changed = true
		jenkins.Spec.Master.Containers[containerIndex].ImagePullPolicy = corev1.PullAlways
	}
	if isResourceRequirementsNotSet(jenkins.Spec.Master.Containers[containerIndex].Resources) {
		logger.Info("Setting default container resource requirements")
		changed = true
		jenkins.Spec.Master.Containers[containerIndex].Resources = resources.NewResourceRequirements("50m", "50Mi", "100m", "100Mi")
	}
	return changed
}

func isResourceRequirementsNotSet(requirements corev1.ResourceRequirements) bool {
	return reflect.DeepEqual(requirements, corev1.ResourceRequirements{})
}

func basePlugins() (result []v1alpha2.Plugin) {
	for _, value := range plugins.BasePlugins() {
		result = append(result, v1alpha2.Plugin{Name: value.Name, Version: value.Version})
	}
	return
}

func (r *JenkinsReconciler) handleDeprecatedData(jenkins *jenkinsv1alpha2.Jenkins) (requeue bool, err error) {
	changed := false
	logger := r.Log.WithValues("cr", jenkins.Name)
	if len(jenkins.Spec.Master.AnnotationsDeprecated) > 0 {
		changed = true
		jenkins.Spec.Master.Annotations = jenkins.Spec.Master.AnnotationsDeprecated
		jenkins.Spec.Master.AnnotationsDeprecated = map[string]string{}
		logger.V(log.VWarn).Info("spec.master.masterAnnotations is deprecated, the annotations have been moved to spec.master.annotations")
	}
	if changed {
		return changed, errors.WithStack(r.Client.Update(context.TODO(), jenkins))
	}
	return changed, nil
}

func (r *JenkinsReconciler) isJenkinsPodTerminating(err error) bool {
	return strings.Contains(err.Error(), "Terminating state with DeletionTimestamp")
}
