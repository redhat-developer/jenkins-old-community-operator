package apis

import (
	"errors"
	"github.com/jenkinsci/kubernetes-operator/pkg/apis/jenkins/v1alpha2"
	routev1 "github.com/openshift/api/route/v1"

	"k8s.io/apimachinery/pkg/runtime"
)

// AddToSchemes may be used to add all resources defined in the project to a Scheme
var AddToSchemes runtime.SchemeBuilder

// AddToScheme adds all Resources to the Scheme
func AddToScheme(s *runtime.Scheme) error {
	err :=  AddToSchemes.AddToScheme(s)
	if err != nil {
		return errors.New("AddToSchemes Not added to scheme")
	}
	err =  routev1.AddToScheme(s)
	if err != nil {
		return errors.New("Route not added to scheme")
	}

	return err
}

func init() {
	// Register the types with the Scheme so the components can map objects to GroupVersionKinds and back
	AddToSchemes = append(AddToSchemes, v1alpha2.SchemeBuilder.AddToScheme)
}
