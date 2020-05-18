package apis

import (
	"github.com/jenkinsci/kubernetes-operator/pkg/apis/jenkins/v1alpha3"
)

func init() {
	// Register the types with the Scheme so the components can map objects to GroupVersionKinds and back
	AddToSchemes = append(AddToSchemes, v1alpha3.SchemeBuilder.AddToScheme)
}
