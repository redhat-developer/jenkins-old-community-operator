package controllers

import (
	"context"

	"github.com/jenkinsci/kubernetes-operator/api/v1alpha2"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:docs-gen:collapse=Imports

const (
	JenkinswBackupName      = "test-jenkins-with-backup"
	JenkinswBackupNamespace = "jenkins-backup-test"
	BackupName              = "test-backup"
	BackupConfigName        = "test-backup-config"
)

var _ = Describe("Backup controller", func() {
	Context("When Creating a Jenkins CR with Backup enabled", func() {
		ctx := context.Background()
		jenkins := GetJenkinswBackupTestInstance(JenkinswBackupName, JenkinswBackupNamespace)
		backupConfig := GetBackupConfigTestInstance(BackupConfigName, JenkinswBackupNamespace, JenkinswBackupName)
		backup := GetBackupTestInstance(BackupName, JenkinswBackupNamespace, BackupConfigName)
		It("Jenkins (with Backup) Deployment Should Be Created", func() {
			Logf("Starting")
			ByCreatingJenkinsWithBackupEnabledSuccessfully(ctx, jenkins)
			ByCheckingThatJenkinsExists(ctx, jenkins)
			ByCheckingThatTheDeploymentExists(ctx, jenkins)
			ByWaitingForDeploymentToBeReady(ctx, jenkins)
		})
		It("Backup is created for Jenkins jobs, plugins and config", func() {
			ByCreatingBackupConfigSucccessfully(ctx, backupConfig)
			ByCreatingBackupSucccessfully(ctx, backup)
		})
	})
})

func GetJenkinswBackupTestInstance(name, namespace string) *v1alpha2.Jenkins {
	// TODO fix e2e to use deployment instead of pod
	annotations := map[string]string{"test": "label"}
	jenkins := &v1alpha2.Jenkins{
		TypeMeta: v1alpha2.JenkinsTypeMeta(),
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: v1alpha2.JenkinsSpec{
			BackupEnabled: true,
		},
	}
	return jenkins
}

func ByCreatingJenkinsWithBackupEnabledSuccessfully(ctx context.Context, jenkins *v1alpha2.Jenkins) {
	By("By creating a new Jenkins with Backup Enabled")
	Expect(k8sClient.Create(ctx, jenkins)).Should(Succeed())
}

func GetBackupConfigTestInstance(name, namespace, jenkinsRef string) *v1alpha2.BackupConfig {
	return &v1alpha2.BackupConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "BackupConfig",
			APIVersion: v1alpha2.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha2.BackupConfigSpec{
			JenkinsRef:            jenkinsRef,
			QuietDownDuringBackup: true,
			Options: v1alpha2.BackupOptions{
				Jobs:    true,
				Plugins: true,
				Config:  true,
			},
			RestartAfterRestore: v1alpha2.RestartConfig{
				Enabled: true,
			},
		},
	}
}

func ByCreatingBackupConfigSucccessfully(ctx context.Context, backupConfig *v1alpha2.BackupConfig) {
	By("By creating a new Backup Config Resource")
	Expect(k8sClient.Create(ctx, backupConfig)).Should(Succeed())
}

func GetBackupTestInstance(name, namespace, backupConfigRef string) *v1alpha2.Backup {
	return &v1alpha2.Backup{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Backup",
			APIVersion: v1alpha2.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha2.BackupSpec{
			ConfigRef: backupConfigRef,
		},
	}
}

func ByCreatingBackupSucccessfully(ctx context.Context, backup *v1alpha2.Backup) {
	By("By creating a new Backup Resource")
	Expect(k8sClient.Create(ctx, backup)).Should(Succeed())
}
