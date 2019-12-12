package jenkins

import (
	"fmt"
	"reflect"

	"github.com/redhat-developer/jenkins-operator/pkg/apis/jenkins/v1alpha2"
	"github.com/redhat-developer/jenkins-operator/pkg/controller/jenkins/constants"
	"github.com/redhat-developer/jenkins-operator/pkg/log"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// enqueueRequestForJenkins enqueues a Request for Secrets and ConfigMaps created by jenkins-operator.
type enqueueRequestForJenkins struct{}

func (e *enqueueRequestForJenkins) Create(evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	if req := e.getOwnerReconcileRequests(evt.Meta); req != nil {
		q.Add(*req)
	}
}

func (e *enqueueRequestForJenkins) Update(evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	req1 := e.getOwnerReconcileRequests(evt.MetaOld)
	req2 := e.getOwnerReconcileRequests(evt.MetaNew)

	if req1 != nil || req2 != nil {
		jenkinsName := "unknown"
		if req1 != nil {
			jenkinsName = req1.Name
		}
		if req2 != nil {
			jenkinsName = req2.Name
		}

		log.Log.WithValues("cr", jenkinsName).Info(
			fmt.Sprintf("%T/%s has been updated", evt.ObjectNew, evt.MetaNew.GetName()))
	}

	if req1 != nil {
		q.Add(*req1)
		return
	}
	if req2 != nil {
		q.Add(*req2)
	}
}

func (e *enqueueRequestForJenkins) Delete(evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
	if req := e.getOwnerReconcileRequests(evt.Meta); req != nil {
		q.Add(*req)
	}
}

func (e *enqueueRequestForJenkins) Generic(evt event.GenericEvent, q workqueue.RateLimitingInterface) {
	if req := e.getOwnerReconcileRequests(evt.Meta); req != nil {
		q.Add(*req)
	}
}

func (e *enqueueRequestForJenkins) getOwnerReconcileRequests(object metav1.Object) *reconcile.Request {
	if object.GetLabels()[constants.LabelAppKey] == constants.LabelAppValue &&
		object.GetLabels()[constants.LabelWatchKey] == constants.LabelWatchValue &&
		len(object.GetLabels()[constants.LabelJenkinsCRKey]) > 0 {
		return &reconcile.Request{NamespacedName: types.NamespacedName{
			Namespace: object.GetNamespace(),
			Name:      object.GetLabels()[constants.LabelJenkinsCRKey],
		}}
	}

	return nil
}

type jenkinsDecorator struct {
	handler handler.EventHandler
}

func (e *jenkinsDecorator) Create(evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	e.handler.Create(evt, q)
}

func (e *jenkinsDecorator) Update(evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	if !reflect.DeepEqual(evt.ObjectOld.(*v1alpha2.Jenkins).Spec, evt.ObjectNew.(*v1alpha2.Jenkins).Spec) {
		log.Log.WithValues("cr", evt.MetaNew.GetName()).Info(
			fmt.Sprintf("%T/%s has been updated", evt.ObjectNew, evt.MetaNew.GetName()))
	}
	e.handler.Update(evt, q)
}

func (e *jenkinsDecorator) Delete(evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
	e.handler.Delete(evt, q)
}

func (e *jenkinsDecorator) Generic(evt event.GenericEvent, q workqueue.RateLimitingInterface) {
	e.handler.Generic(evt, q)
}
