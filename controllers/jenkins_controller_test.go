package controllers

import (
	"context"
	"time"

	"github.com/jenkinsci/kubernetes-operator/api/v1alpha2"
	"github.com/jenkinsci/kubernetes-operator/pkg/configuration/base/resources"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// +kubebuilder:docs-gen:collapse=Imports

const (
	// Name                  = "test-image"
	JenkinsName      = "test-jenkins"
	JenkinsNamespace = "jenkins-test"

	timeout  = time.Second * 60
	interval = time.Millisecond * 250
)

var _ = Describe("Jenkins controller", func() {
	Logf("Starting")
	Context("When Creating a Jenkins CR", func() {
		ctx := context.Background()
		jenkins := GetJenkinsTestInstance(JenkinsName, JenkinsNamespace)
		It("Deployment Should Be Created", func() {
			ByCreatingJenkinsSuccesfully(ctx, jenkins)
			ByCheckingThatJenkinsExists(ctx, jenkins)
			ByCheckingThatTheDeploymentExists(ctx, jenkins)
		})
	})

	Context("When Creating a Jenkins CR With specified Master.Image", func() {
		ctx := context.Background()
		image := "my-image"
		jenkinsWithImage := "jenkins-with-image"
		jenkins := GetJenkinsTestInstanceWithMasterImage(jenkinsWithImage, JenkinsNamespace, image)
		It("Deployment Should Be Created With That Image Name", func() {
			ByCreatingJenkinsSuccesfully(ctx, jenkins)
			ByCheckingThatJenkinsExists(ctx, jenkins)
			ByCheckingThatTheDeploymentExists(ctx, jenkins)
			ByCheckingThatDeploymentImageIs(ctx, jenkins)
		})
	})
})

func ByCheckingThatDeploymentImageIs(ctx context.Context, jenkins *v1alpha2.Jenkins) {
	By("By checking that the Jenkins exists")
	created := &v1alpha2.Jenkins{}
	expectedName := jenkins.Name
	key := types.NamespacedName{Namespace: jenkins.Namespace, Name: expectedName}
	actual := func() (string, error) {
		err := k8sClient.Get(ctx, key, created)
		if err != nil {
			return "", err
		}
		return created.Spec.Master.Containers[0].Image, nil
	}
	Eventually(actual, timeout, interval).Should(Equal(jenkins.Spec.Master.Containers[0].Image))
}

func GetJenkinsTestInstanceWithMasterImage(jenkinsName string, namespaceName string, imageName string) *v1alpha2.Jenkins {
	jenkins := GetJenkinsTestInstance(jenkinsName, namespaceName)
	jenkins.Spec.Master.Containers[0].Image = imageName
	return jenkins
}

func ByCheckingThatJenkinsExists(ctx context.Context, jenkins *v1alpha2.Jenkins) {
	By("By checking that the Jenkins exists")
	created := &v1alpha2.Jenkins{}
	expectedName := jenkins.Name
	key := types.NamespacedName{Namespace: jenkins.Namespace, Name: expectedName}
	actual := func() (*v1alpha2.Jenkins, error) {
		err := k8sClient.Get(ctx, key, created)
		if err != nil {
			return nil, err
		}
		return created, nil
	}
	Eventually(actual, timeout, interval).Should(Equal(created))
}

func ByCheckingThatTheDeploymentExists(ctx context.Context, jenkins *v1alpha2.Jenkins) {
	By("By checking that the Pod exists")
	expected := &appsv1.Deployment{}
	expectedName := resources.GetJenkinsDeploymentName(jenkins)
	key := types.NamespacedName{Namespace: jenkins.Namespace, Name: expectedName}
	actual := func() (*appsv1.Deployment, error) {
		err := k8sClient.Get(ctx, key, expected)
		if err != nil {
			return nil, err
		}
		return expected, nil
	}
	Eventually(actual, timeout, interval).ShouldNot(BeNil())
}

func ByCreatingJenkinsSuccesfully(ctx context.Context, jenkins *v1alpha2.Jenkins) {
	By("By creating a new Jenkins")
	Expect(k8sClient.Create(ctx, jenkins)).Should(Succeed())
}

func ByWaitingForDeploymentToBeReady(ctx context.Context, jenkins *v1alpha2.Jenkins) {
	By("By checking that the Pod exists")
	expected := &appsv1.Deployment{}
	var expectedReadyReplicas int32 = 1
	expectedName := resources.GetJenkinsDeploymentName(jenkins)
	key := types.NamespacedName{Namespace: jenkins.Namespace, Name: expectedName}
	actualReplicas := func() (readyReplicas int32) {
		err := k8sClient.Get(ctx, key, expected)
		if err != nil {
			return expected.Status.ReadyReplicas
		}
		return expected.Status.ReadyReplicas
	}
	Eventually(actualReplicas, timeout, interval).Should(BeNumerically("==", expectedReadyReplicas))
}

func GetJenkinsTestInstance(name string, namespace string) *v1alpha2.Jenkins {
	// TODO fix e2e to use deployment instead of pod
	annotations := map[string]string{"test": "label"}
	jenkins := &v1alpha2.Jenkins{
		TypeMeta: v1alpha2.JenkinsTypeMeta(),
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: v1alpha2.JenkinsSpec{},
	}
	return jenkins
}
