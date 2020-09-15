package resources

import (
	"fmt"

	"github.com/jenkinsci/kubernetes-operator/pkg/apis/jenkins/v1alpha2"
	"github.com/jenkinsci/kubernetes-operator/pkg/constants"
	routev1 "github.com/openshift/api/route/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes"
)

//RouteKind the kind name for route
const RouteKind = "Route"

var isRouteAPIAvailable = false
var routeAPIChecked = false

// NewJenkinsIngress takes the ServiceName and Creates the Route based on it
func NewJenkinsIngress(meta metav1.ObjectMeta, jenkins *v1alpha2.Jenkins) *extensions.Ingress {
	name := GetJenkinsIngressName(jenkins.Name)
	serviceName := GetJenkinsHTTPServiceName(jenkins)
	emptyTLS := extensions.IngressTLS{
		Hosts: nil,
	}
	ingress := extensions.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: meta.Namespace,
			Labels:    meta.Labels,
		},
		Spec: extensions.IngressSpec{
			TLS: []extensions.IngressTLS{emptyTLS},
			Rules: []extensions.IngressRule{{
				Host: jenkins.Spec.Host,
				IngressRuleValue: extensions.IngressRuleValue{
					HTTP: &extensions.HTTPIngressRuleValue{
						Paths: []extensions.HTTPIngressPath{{
							Backend: extensions.IngressBackend{
								ServiceName: serviceName,
								ServicePort: intstr.FromInt(int(jenkins.Spec.Service.Port)),
							},
						}},
					},
				},
			}},
		},
	}
	return &ingress
}

// GetJenkinsIngressName returns Jenkins ingress name for given CR
func GetJenkinsIngressName(name string) string {
	return fmt.Sprintf("%s-%s-%s", constants.LabelAppValue, name, constants.IngressTypeName)
}

// UpdateRoute returns new route matching the service
func UpdateRoute(actual routev1.Route, jenkins *v1alpha2.Jenkins) routev1.Route {
	actualTargetService := actual.Spec.To
	serviceName := GetJenkinsHTTPServiceName(jenkins)
	if actualTargetService.Name != serviceName {
		actual.Spec.To.Name = serviceName
	}
	port := jenkins.Spec.Service.Port
	if actual.Spec.Port.TargetPort.IntVal != port {
		actual.Spec.Port.TargetPort = intstr.FromInt(int(port))
	}
	return actual
}

//IsRouteAPIAvailable tells if the Route API is installed and discoverable
func IsRouteAPIAvailable(clientSet *kubernetes.Clientset) bool {
	if routeAPIChecked {
		return isRouteAPIAvailable
	}
	gv := schema.GroupVersion{
		Group:   routev1.GroupName,
		Version: routev1.SchemeGroupVersion.Version,
	}
	if err := discovery.ServerSupportsVersion(clientSet, gv); err != nil {
		// error, API not available
		routeAPIChecked = true
		isRouteAPIAvailable = false
	} else {
		// API Exists
		routeAPIChecked = true
		isRouteAPIAvailable = true
	}
	return isRouteAPIAvailable
}
