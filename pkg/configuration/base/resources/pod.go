package resources

import (
	"context"
	"fmt"
	"time"

	"github.com/jenkinsci/kubernetes-operator/api/v1alpha2"
	"github.com/jenkinsci/kubernetes-operator/pkg/constants"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/kubernetes/pkg/client/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	JenkinsMasterContainerName = constants.DefaultJenkinsMasterContainerName
	// JenkinsHomeVolumeName is the Jenkins home volume name
	JenkinsHomeVolumeName = "jenkins-home"
	jenkinsPath           = "/var/jenkins"

	jenkinsScriptsVolumeName = "scripts"
	// JenkinsScriptsVolumePath is a path where are scripts used to configure Jenkins
	JenkinsScriptsVolumePath = jenkinsPath + "/scripts"
	// InitScriptName is the init script name which configures init.groovy.d, scripts and install plugins
	InitScriptName = "init.sh"

	jenkinsOperatorCredentialsVolumeName = "operator-credentials"
	jenkinsOperatorCredentialsVolumePath = jenkinsPath + "/operator-credentials"

	jenkinsInitConfigurationVolumeName = "init-configuration"
	jenkinsInitConfigurationVolumePath = jenkinsPath + "/init-configuration"

	ConfigurationAsCodeVolumeName       = "casc-config"
	ConfigurationAsCodeVolumePath       = jenkinsPath + "/configuration-as-code"
	ConfigurationAsCodeSecretVolumeName = "casc-secret"
	ConfigurationAsCodeSecretVolumePath = "/tmp" + "/configuration-as-code-secrets"

	httpPortName = "http"
	jnlpPortName = "jnlp"

	// defaut configmap for jenkins configuration
	JenkinsDefaultConfigMapName = "jenkins-default-configuration"

	// Names of Sidecar and Init Containers
	ConfigSidecarName       = "config"
	ConfigInitContainerName = "config-init"
	BackupSidecarName       = "backup"
	BackupInitContainerName = "backup-init"
	// Config Sidecar related variables
	JenkinsSCConfigImage      = "kiwigrid/k8s-sidecar:0.1.144"
	JenkinsSCConfigReqURL     = "http://localhost:8080/reload-configuration-as-code/?casc-reload-token=$(POD_NAME)"
	JenkinsSCConfigReqMethod  = "POST"
	JenkinsSCConfigReqRetry   = "10"
	JenkinsSCConfigLabel      = "type"
	JenkinsSCConfigLabelValue = "%s-jenkins-config"
	// Backup Sidecar related variables
	JenkinsBackupVolumeMountName = "backup-pool"
	JenkinsBackupVolumePath      = "/jenkins-backups"
	// Helper scripts related variables
	ScriptsVolumeMountName    = "helper-scripts"
	ScriptsVolumePath         = "/jenkins-operator-scripts"
	QuietDownScriptPath       = ScriptsVolumePath + "/quietdown.sh"
	CancelQuietDownScriptPath = ScriptsVolumePath + "/cancelquietdown.sh"
	RestartScriptPath         = ScriptsVolumePath + "/restart.sh"
	SafeRestartScriptPath     = ScriptsVolumePath + "/saferestart.sh"
	// Common attributes used for Sidecars
	SidecarCPULimit   = "100m"
	SidecarMEMLimit   = "100Mi"
	SidecarCPURequest = "50m"
	SidecarMEMRequest = "50Mi"
	// Images
	UBIMinimalImage = "ubi8/ubi-minimal:latest"
)

// GetJenkinsMasterContainerBaseEnvs returns Jenkins master pod envs required by operator
func GetJenkinsMasterContainerBaseEnvs(jenkins *v1alpha2.Jenkins) []corev1.EnvVar {
	envVars := []corev1.EnvVar{
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					APIVersion: "v1",
					FieldPath:  "metadata.name",
				},
			},
		},
		{
			Name:  "OPENSHIFT_ENABLE_OAUTH",
			Value: "true",
		},
		{
			Name:  "OPENSHIFT_ENABLE_REDIRECT_PROMPT",
			Value: "true",
		},
		{
			Name:  "COPY_REFERENCE_FILE_LOG",
			Value: fmt.Sprintf("%s/%s", getJenkinsHomePath(jenkins), "copy_reference_file.log"),
		},
	}

	spec := jenkins.Status.Spec
	if spec.ConfigurationAsCode != nil {
		if len(spec.ConfigurationAsCode.Secret.Name) > 0 {
			envVars = append(envVars, corev1.EnvVar{
				Name:  "SECRETS",
				Value: ConfigurationAsCodeSecretVolumePath,
			})
		}

		if spec.ConfigurationAsCode.Enabled {
			envVars = append(envVars, corev1.EnvVar{
				Name:  "CASC_JENKINS_CONFIG",
				Value: ConfigurationAsCodeVolumePath,
			})
		}
	}
	return envVars
}

// getJenkinsHomePath fetches the Home Path for Jenkins
func getJenkinsHomePath(jenkins *v1alpha2.Jenkins) string {
	defaultJenkinsHomePath := "/var/lib/jenkins"
	for _, envVar := range jenkins.Status.Spec.Master.Containers[0].Env {
		if envVar.Name == "JENKINS_HOME" {
			return envVar.Value
		}
	}
	return defaultJenkinsHomePath
}

// GetJenkinsMasterPodBaseVolumes returns Jenkins master pod volumes required by operator
func GetJenkinsMasterPodBaseVolumes(jenkins *v1alpha2.Jenkins) []corev1.Volume {
	configMapVolumeSourceDefaultMode := corev1.ConfigMapVolumeSourceDefaultMode
	secretVolumeSourceDefaultMode := corev1.SecretVolumeSourceDefaultMode
	var scriptsVolumeDefaultMode int32 = 0777
	volumes := []corev1.Volume{
		{
			Name: JenkinsHomeVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: jenkinsScriptsVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					DefaultMode: &scriptsVolumeDefaultMode,
					LocalObjectReference: corev1.LocalObjectReference{
						Name: getScriptsConfigMapName(jenkins),
					},
				},
			},
		},
		{
			Name: jenkinsInitConfigurationVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					DefaultMode: &configMapVolumeSourceDefaultMode,
					LocalObjectReference: corev1.LocalObjectReference{
						Name: GetInitConfigurationConfigMapName(jenkins),
					},
				},
			},
		},
		{
			Name: jenkinsOperatorCredentialsVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					DefaultMode: &secretVolumeSourceDefaultMode,
					SecretName:  GetOperatorCredentialsSecretName(jenkins),
				},
			},
		},
	}
	if jenkins.Status != nil && jenkins.Status.Spec != nil && jenkins.Status.Spec.ConfigurationAsCode != nil {
		spec := jenkins.Status.Spec
		configurationAsCode := spec.ConfigurationAsCode
		if configurationAsCode.Enabled {
			// target volume for the init container
			// All casc configmaps will be copied here
			volumes = append(volumes, corev1.Volume{
				Name: ConfigurationAsCodeVolumeName,
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			})
			// volume for default config configmap
			if configurationAsCode.DefaultConfig {
				volumes = append(volumes, corev1.Volume{
					Name: fmt.Sprintf("casc-default-%s", JenkinsDefaultConfigMapName),
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							DefaultMode: &configMapVolumeSourceDefaultMode,
							LocalObjectReference: corev1.LocalObjectReference{
								Name: JenkinsDefaultConfigMapName,
							},
						},
					},
				})
			}
			// Loop to add all casc configmap volumes
			for _, cm := range configurationAsCode.Configurations {
				volumes = append(volumes, corev1.Volume{
					Name: fmt.Sprintf("casc-init-%s", cm.Name),
					VolumeSource: corev1.VolumeSource{
						ConfigMap: &corev1.ConfigMapVolumeSource{
							DefaultMode: &configMapVolumeSourceDefaultMode,
							LocalObjectReference: corev1.LocalObjectReference{
								Name: cm.Name,
							},
						},
					},
				})
			}
			// Add casc secret volume
			if len(configurationAsCode.Secret.Name) > 0 {
				volumes = append(volumes, corev1.Volume{
					Name: ConfigurationAsCodeSecretVolumeName,
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							DefaultMode: &secretVolumeSourceDefaultMode,
							SecretName:  configurationAsCode.Secret.Name,
						},
					},
				})
			}
		}
	}
	return volumes
}

// GetJenkinsMasterContainerBaseVolumeMounts returns Jenkins master pod volume mounts required by operator
func GetJenkinsMasterContainerBaseVolumeMounts(jenkins *v1alpha2.Jenkins, spec *v1alpha2.JenkinsSpec) []corev1.VolumeMount {
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      JenkinsHomeVolumeName,
			MountPath: getJenkinsHomePath(jenkins),
			ReadOnly:  false,
		},
		{
			Name:      jenkinsScriptsVolumeName,
			MountPath: JenkinsScriptsVolumePath,
			ReadOnly:  true,
		},
		{
			Name:      jenkinsInitConfigurationVolumeName,
			MountPath: jenkinsInitConfigurationVolumePath,
			ReadOnly:  true,
		},
		{
			Name:      jenkinsOperatorCredentialsVolumeName,
			MountPath: jenkinsOperatorCredentialsVolumePath,
			ReadOnly:  true,
		},
	}

	if spec.ConfigurationAsCode != nil {
		if spec.ConfigurationAsCode.Enabled {
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      ConfigurationAsCodeVolumeName,
				MountPath: ConfigurationAsCodeVolumePath,
				ReadOnly:  false,
			})
			if len(spec.ConfigurationAsCode.Secret.Name) > 0 {
				volumeMounts = append(volumeMounts, corev1.VolumeMount{
					Name:      ConfigurationAsCodeSecretVolumeName,
					MountPath: ConfigurationAsCodeSecretVolumePath,
					ReadOnly:  false,
				})
			}
		}
	}
	return volumeMounts
}

// NewJenkinsMasterContainer returns Jenkins master Kubernetes container
func NewJenkinsMasterContainer(jenkins *v1alpha2.Jenkins) corev1.Container {
	jenkinsContainer := jenkins.Status.Spec.Master.Containers[0]

	envs := GetJenkinsMasterContainerBaseEnvs(jenkins)
	envs = append(envs, jenkinsContainer.Env...)

	jenkinsHomeEnvVar := corev1.EnvVar{
		Name:  "JENKINS_HOME",
		Value: getJenkinsHomePath(jenkins),
	}

	jenkinsHomeEnvVarExists := false
	for _, env := range jenkinsContainer.Env {
		if env.Name == jenkinsHomeEnvVar.Name {
			jenkinsHomeEnvVarExists = true

			break
		}
	}

	if !jenkinsHomeEnvVarExists {
		envs = append(envs, jenkinsHomeEnvVar)
	}

	return GetJenkinsContainer(jenkins, jenkinsContainer, envs)
}

func GetJenkinsContainer(jenkins *v1alpha2.Jenkins, jenkinsContainer v1alpha2.Container, envs []corev1.EnvVar) corev1.Container {
	container := corev1.Container{
		Name:            JenkinsMasterContainerName,
		Image:           jenkinsContainer.Image,
		ImagePullPolicy: jenkinsContainer.ImagePullPolicy,
		LivenessProbe:   jenkinsContainer.LivenessProbe,
		ReadinessProbe:  jenkinsContainer.ReadinessProbe,
		Ports: []corev1.ContainerPort{
			{
				Name:          httpPortName,
				ContainerPort: constants.DefaultHTTPPortInt32,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          jnlpPortName,
				ContainerPort: constants.DefaultJNLPPortInt32,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		SecurityContext: jenkinsContainer.SecurityContext,
		Env:             envs,
		Resources:       jenkinsContainer.Resources,
		VolumeMounts:    append(GetJenkinsMasterContainerBaseVolumeMounts(jenkins, jenkins.Status.Spec), jenkinsContainer.VolumeMounts...),
	}
	if jenkinsContainer.Command != nil {
		container.Command = jenkinsContainer.Command
	}
	return container
}

// NewJenkinsConfigContainer returns Jenkins side container for config reloading
func NewJenkinsConfigContainer(jenkins *v1alpha2.Jenkins) corev1.Container {
	envs := map[string]string{
		"LABEL":             JenkinsSCConfigLabel,
		"LABEL_VALUE":       fmt.Sprintf(JenkinsSCConfigLabelValue, jenkins.Name),
		"FOLDER":            ConfigurationAsCodeVolumePath,
		"REQ_URL":           JenkinsSCConfigReqURL,
		"REQ_METHOD":        JenkinsSCConfigReqMethod,
		"REQ_RETRY_CONNECT": JenkinsSCConfigReqRetry,
	}

	envVars := []corev1.EnvVar{
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
	}

	for k, v := range envs {
		envVars = append(envVars, corev1.EnvVar{
			Name:  k,
			Value: v,
		})
	}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      ConfigurationAsCodeVolumeName,
			MountPath: ConfigurationAsCodeVolumePath,
			ReadOnly:  false,
		},
		{
			Name:      JenkinsHomeVolumeName,
			MountPath: getJenkinsHomePath(jenkins),
			ReadOnly:  true,
		},
	}

	return corev1.Container{
		Name:            ConfigSidecarName,
		Image:           JenkinsSCConfigImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             envVars,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(SidecarCPURequest),
				corev1.ResourceMemory: resource.MustParse(SidecarMEMRequest),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(SidecarCPULimit),
				corev1.ResourceMemory: resource.MustParse(SidecarMEMLimit),
			},
		},
		VolumeMounts: volumeMounts,
	}
}

func NewJenkinsBackupContainer(jenkins *v1alpha2.Jenkins) corev1.Container {
	backupContainer := corev1.Container{
		Name:            BackupSidecarName,
		Image:           UBIMinimalImage,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"/bin/sh", "-c", "--"},
		Args:            []string{"while true; do sleep 30; done;"},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      JenkinsBackupVolumeMountName,
				ReadOnly:  false,
				MountPath: JenkinsBackupVolumePath,
			},
			{
				Name:      ScriptsVolumeMountName,
				ReadOnly:  false,
				MountPath: ScriptsVolumePath,
			},
			{
				Name:      JenkinsHomeVolumeName,
				ReadOnly:  false,
				MountPath: getJenkinsHomePath(jenkins),
			},
		},
		Stdin: true,
		TTY:   true,
	}

	return backupContainer
}

// NewJenkinsConfigInitContainer returns Jenkins init container to copy configmap to make it writable
func NewJenkinsConfigInitContainer(spec *v1alpha2.JenkinsSpec) corev1.Container {
	jenkinsContainer := spec.Master.Containers[0]
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      ConfigurationAsCodeVolumeName,
			MountPath: ConfigurationAsCodeVolumePath,
			ReadOnly:  false,
		},
	}

	if spec.ConfigurationAsCode == nil || spec.ConfigurationAsCode.DefaultConfig {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      fmt.Sprintf("casc-default-%s", JenkinsDefaultConfigMapName),
			MountPath: jenkinsPath + fmt.Sprintf("/casc-default-%s", JenkinsDefaultConfigMapName),
			ReadOnly:  false,
		})
	}

	if spec.ConfigurationAsCode != nil && spec.ConfigurationAsCode.Enabled {
		for _, cm := range spec.ConfigurationAsCode.Configurations {
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      fmt.Sprintf("casc-init-%s", cm.Name),
				MountPath: jenkinsPath + fmt.Sprintf("/casc-init-%s", cm.Name),
				ReadOnly:  false,
			})
		}
	}

	command := []string{
		"bash",
		"-c",
		fmt.Sprintf("if [ `ls %s/casc-* > /dev/null 2>&1; echo $?` -eq 0 ]; then find %s/casc-* -type f -exec cp -fL {} %s \\;; fi",
			jenkinsPath, jenkinsPath, ConfigurationAsCodeVolumePath),
	}
	return corev1.Container{
		Name:            ConfigInitContainerName,
		Image:           jenkinsContainer.Image,
		ImagePullPolicy: jenkinsContainer.ImagePullPolicy,
		Command:         command,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(SidecarCPURequest),
				corev1.ResourceMemory: resource.MustParse(SidecarMEMRequest),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(SidecarCPULimit),
				corev1.ResourceMemory: resource.MustParse(SidecarMEMLimit),
			},
		},
		VolumeMounts: volumeMounts,
	}
}

// NewJenkinsConfigInitContainer returns Jenkins init container to copy configmap to make it writable
func NewJenkinsBackupInitContainer(spec *v1alpha2.JenkinsSpec) corev1.Container {
	jenkinsContainer := spec.Master.Containers[0]
	volumeMounts := []corev1.VolumeMount{
		{
			Name:      JenkinsBackupVolumeMountName,
			ReadOnly:  false,
			MountPath: JenkinsBackupVolumePath,
		},
		{
			Name:      ScriptsVolumeMountName,
			ReadOnly:  false,
			MountPath: ScriptsVolumePath,
		},
	}

	scriptTemplate := `cat > %s << %s
SERVER=http://localhost:8080
CRUMB=\$(curl --user \$USER:\$APITOKEN \$SERVER/crumbIssuer/api/xml?xpath=concat\(//crumbRequestField,%%22:%%22,//crumb\)) 
curl -X POST --user \$USER:\$APITOKEN -H "\$CRUMB" \$SERVER/%s
%s
`
	heredoc := "heredoc"
	quietDown := "quietDown"
	cancelQuietDown := "cancelQuietDown"
	restart := "restart"
	safeRestart := "safeRestart"

	quietDownHereDoc := quietDown + heredoc
	cancelQuietDownHereDoc := cancelQuietDown + heredoc
	restartHereDoc := restart + heredoc
	safeRestartHereDoc := safeRestart + heredoc

	quietDownScript := fmt.Sprintf(scriptTemplate, QuietDownScriptPath, quietDownHereDoc, quietDown, quietDownHereDoc)
	cancelQuietDownScript := fmt.Sprintf(scriptTemplate, CancelQuietDownScriptPath, cancelQuietDownHereDoc, cancelQuietDown, cancelQuietDownHereDoc)
	restartScript := fmt.Sprintf(scriptTemplate, RestartScriptPath, restartHereDoc, restart, restartHereDoc)
	safeRestartScript := fmt.Sprintf(scriptTemplate, SafeRestartScriptPath, safeRestartHereDoc, safeRestart, safeRestartHereDoc)

	commandString := fmt.Sprintf(`%s
%s 
%s
%s
`, quietDownScript, cancelQuietDownScript, restartScript, safeRestartScript)

	command := []string{"bash", "-c", commandString}

	return corev1.Container{
		Name:            BackupInitContainerName,
		Image:           jenkinsContainer.Image,
		ImagePullPolicy: jenkinsContainer.ImagePullPolicy,
		Command:         command,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(SidecarCPURequest),
				corev1.ResourceMemory: resource.MustParse(SidecarMEMRequest),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(SidecarCPULimit),
				corev1.ResourceMemory: resource.MustParse(SidecarMEMLimit),
			},
		},
		VolumeMounts: volumeMounts,
	}
}

// ConvertJenkinsContainerToKubernetesContainer converts Jenkins container to Kubernetes container
func ConvertJenkinsContainerToKubernetesContainer(container v1alpha2.Container) corev1.Container {
	return corev1.Container{
		Name:            container.Name,
		Image:           container.Image,
		Command:         container.Command,
		Args:            container.Args,
		WorkingDir:      container.WorkingDir,
		Ports:           container.Ports,
		EnvFrom:         container.EnvFrom,
		Env:             container.Env,
		Resources:       container.Resources,
		VolumeMounts:    container.VolumeMounts,
		LivenessProbe:   container.LivenessProbe,
		ReadinessProbe:  container.ReadinessProbe,
		Lifecycle:       container.Lifecycle,
		ImagePullPolicy: container.ImagePullPolicy,
		SecurityContext: container.SecurityContext,
	}
}

func newContainers(jenkins *v1alpha2.Jenkins, spec *v1alpha2.JenkinsSpec) (containers []corev1.Container) {
	containers = append(containers, NewJenkinsMasterContainer(jenkins))
	if spec.ConfigurationAsCode != nil {
		if spec.ConfigurationAsCode.Enabled && spec.ConfigurationAsCode.EnableAutoReload {
			containers = append(containers, NewJenkinsConfigContainer(jenkins))
		}
		for _, container := range spec.Master.Containers[1:] {
			containers = append(containers, ConvertJenkinsContainerToKubernetesContainer(container))
		}
	}
	if spec.BackupEnabled {
		containers = append(containers, NewJenkinsBackupContainer(jenkins))
	}

	return containers
}

func newInitContainers(jenkinsSpec *v1alpha2.JenkinsSpec) (containers []corev1.Container) {
	if jenkinsSpec.ConfigurationAsCode == nil || jenkinsSpec.ConfigurationAsCode.Enabled {
		containers = append(containers, NewJenkinsConfigInitContainer(jenkinsSpec))
	}
	if jenkinsSpec.BackupEnabled {
		containers = append(containers, NewJenkinsBackupInitContainer(jenkinsSpec))
	}
	return containers
}

func GetJenkinsBackupPVCName(jenkins *v1alpha2.Jenkins) string {
	return jenkins.Name + "-jenkins-backup"
}

// return a condition function that indicates whether the given pod is
// currently running
func isPodRunning(k8sClient client.Client, podName, namespace string) wait.ConditionFunc {
	return func() (bool, error) {
		pod := &corev1.Pod{}
		err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: podName, Namespace: namespace}, pod)
		if err != nil {
			return false, err
		}

		switch pod.Status.Phase {
		case corev1.PodRunning:
			return true, nil
		case corev1.PodFailed, corev1.PodSucceeded, corev1.PodPending, corev1.PodUnknown:
			return false, conditions.ErrPodCompleted
		}
		return false, nil
	}
}

// return a condition function that indicates whether the given pod is
// currently running
func isPodCompleted(k8sClient client.Client, podName, namespace string) wait.ConditionFunc {
	return func() (bool, error) {
		pod := &corev1.Pod{}
		err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: podName, Namespace: namespace}, pod)
		if err != nil {
			return false, err
		}
		switch pod.Status.Phase {
		case corev1.PodSucceeded:
			return true, nil
		case corev1.PodFailed, corev1.PodRunning, corev1.PodPending, corev1.PodUnknown:
			return false, nil
		}
		return false, nil
	}
}

// Poll up to timeout seconds for pod to enter running state.
// Returns an error if the pod never enters the running state.
func WaitForPodRunning(k8sClient client.Client, podName, namespace string, timeout time.Duration) error {
	return wait.PollImmediate(time.Second, timeout, isPodRunning(k8sClient, podName, namespace))
}

// Poll up to timeout seconds for pod to enter running state.
// Returns an error if the pod never enters the running state.
func WaitForPodIsCompleted(k8sClient client.Client, podName, namespace string) error {
	return wait.PollUntil(time.Second, isPodCompleted(k8sClient, podName, namespace), nil)
}
