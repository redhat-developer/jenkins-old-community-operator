package base

import (
	"context"
	"fmt"

	"github.com/jenkinsci/kubernetes-operator/pkg/configuration/base/resources"
	"github.com/jenkinsci/kubernetes-operator/pkg/constants"
	"github.com/jenkinsci/kubernetes-operator/pkg/log"
	"github.com/jenkinsci/kubernetes-operator/pkg/notifications/event"
	"github.com/jenkinsci/kubernetes-operator/pkg/notifications/reason"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	extensions "k8s.io/api/extensions/v1beta1"

	"github.com/jenkinsci/kubernetes-operator/pkg/apis/jenkins/v1alpha2"
	"k8s.io/apimachinery/pkg/util/intstr"

	routev1 "github.com/openshift/api/route/v1"
	stackerr "github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func (r *ReconcileJenkinsBaseConfiguration) ensureJenkinsIngresstIsPresent(meta metav1.ObjectMeta) (reconcile.Result, error) {
	resource, err := r.GetJenkinsIngress()
	if apierrors.IsNotFound(err) {
		r.logger.Info(fmt.Sprintf("Error while getting the Ingress %s", err))
		resource = resources.NewJenkinsIngress(meta, r.Configuration.Jenkins)
		resourceName := resource.Name
		namespace := resource.Namespace
		r.sendIngressCreationNotification()
		r.logger.Info(fmt.Sprintf("Creating a new Ingress %s/%s", namespace, resourceName))
		err := r.CreateResource(resource)
		if err != nil {
			r.logger.Info(fmt.Sprintf("Error while creating Ingress %s: %s", resourceName, err))
			return reconcile.Result{Requeue: true}, stackerr.WithStack(err)
		}
		r.logger.Info(fmt.Sprintf("Deployment %s successfully created", resourceName))
		r.sendSuccessfulIngressCreationNotification(resourceName)
		jenkinsName := r.Jenkins.Name
		creationTimestamp := resource.CreationTimestamp
		r.logger.Info(fmt.Sprintf("Updating Jenkins %s to set UserAndPassword and ProvisionStartTime to %+v", jenkinsName, creationTimestamp))
	} else if err != nil {
		resourceName := resource.Name
		r.logger.Info(fmt.Sprintf("Error while getting Ingress %s and error type is different from not found: %s", resourceName, err))
		return reconcile.Result{Requeue: false}, stackerr.WithStack(err)
	}
	r.logger.Info(fmt.Sprintf("Ingress %s exist or has been created without any error", resource.Name))
	return reconcile.Result{Requeue: false}, nil
}

// createRoute takes the ServiceName and Creates the Route based on it
func (r *ReconcileJenkinsBaseConfiguration) createRoute(meta metav1.ObjectMeta, serviceName string, config *v1alpha2.Jenkins) error {
	name := fmt.Sprintf("%s-%s", constants.LabelAppValue, config.ObjectMeta.Name)
	r.logger.Info(fmt.Sprintf("Creating a new Route %s", name))
	route := routev1.Route{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: meta.Namespace}, &route)
	if err != nil && apierrors.IsNotFound(err) {
		port := &routev1.RoutePort{
			TargetPort: intstr.FromString(""),
		}

		routeSpec := routev1.RouteSpec{
			TLS: &routev1.TLSConfig{
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
				Termination:                   routev1.TLSTerminationEdge,
			},
			To: routev1.RouteTargetReference{
				Kind: resources.ServiceKind,
				Name: serviceName,
			},
			Port: port,
		}
		actual := routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: meta.Namespace,
				Labels:    meta.Labels,
			},
			Spec: routeSpec,
		}
		route = resources.UpdateRoute(actual, config)
		if err = r.CreateResource(&route); err != nil {
			return stackerr.WithStack(err)
		}
	} else if err != nil {
		return stackerr.WithStack(err)
	}

	route.ObjectMeta.Labels = meta.Labels // make sure that user won't break service by hand
	route = resources.UpdateRoute(route, config)
	return stackerr.WithStack(r.UpdateResource(&route))
}

// GetJenkinsIngress gets the jenkins Ingress
func (r *ReconcileJenkinsBaseConfiguration) GetJenkinsIngress() (*extensions.Ingress, error) {
	r.logger.V(log.VDebug).Info(fmt.Sprintf("Getting Ingress for : %+v", r.Jenkins.Name))
	ingressName := resources.GetJenkinsIngressName(r.Jenkins.Name)
	currentIngress := &extensions.Ingress{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: ingressName, Namespace: r.Jenkins.Namespace}, currentIngress)
	if err != nil {
		r.logger.V(log.VDebug).Info(fmt.Sprintf("Ingress '%s' not found", ingressName))
		return nil, err
	}
	return currentIngress, nil
}

func (r *ReconcileJenkinsBaseConfiguration) sendSuccessfulIngressCreationNotification(deploymentName string) {
	shortMessage := fmt.Sprintf("Ingress %s successfully created", deploymentName)
	*r.Notifications <- event.Event{
		Jenkins: *r.Configuration.Jenkins,
		Phase:   event.PhaseBase,
		Level:   v1alpha2.NotificationLevelInfo,
		Reason:  reason.NewDeploymentEvent(reason.OperatorSource, []string{shortMessage}),
	}
}

func (r *ReconcileJenkinsBaseConfiguration) sendIngressCreationNotification() {
	*r.Notifications <- event.Event{
		Jenkins: *r.Configuration.Jenkins,
		Phase:   event.PhaseBase,
		Level:   v1alpha2.NotificationLevelInfo,
		Reason:  reason.NewDeploymentEvent(reason.OperatorSource, []string{"Creating a Jenkins Ingress"}),
	}
}
