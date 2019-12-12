package resources

import (
	"fmt"
	"text/template"

	"github.com/redhat-developer/jenkins-operator/internal/render"
	"github.com/redhat-developer/jenkins-operator/pkg/apis/jenkins/v1alpha2"
	"github.com/redhat-developer/jenkins-operator/pkg/controller/jenkins/constants"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const createOperatorUserFileName = "createOperatorUser.groovy"

var createOperatorUserGroovyFmtTemplate = template.Must(template.New(createOperatorUserFileName).Parse(`
import hudson.security.*

def jenkins = jenkins.model.Jenkins.getInstance()
def operatorUserCreatedFile = new File('{{ .OperatorUserCreatedFilePath }}')

if (!operatorUserCreatedFile.exists()) {
	def hudsonRealm = new HudsonPrivateSecurityRealm(false)
	hudsonRealm.createAccount(
		new File('{{ .OperatorCredentialsPath }}/{{ .OperatorUserNameFile }}').text,
		new File('{{ .OperatorCredentialsPath }}/{{ .OperatorPasswordFile }}').text)
	jenkins.setSecurityRealm(hudsonRealm)

	def strategy = new FullControlOnceLoggedInAuthorizationStrategy()
	strategy.setAllowAnonymousRead(false)
	jenkins.setAuthorizationStrategy(strategy)
	jenkins.save()

	operatorUserCreatedFile.createNewFile()
}
`))

func buildCreateJenkinsOperatorUserGroovyScript(jenkins *v1alpha2.Jenkins) (*string, error) {
	data := struct {
		OperatorCredentialsPath     string
		OperatorUserNameFile        string
		OperatorPasswordFile        string
		OperatorUserCreatedFilePath string
	}{
		OperatorCredentialsPath:     jenkinsOperatorCredentialsVolumePath,
		OperatorUserNameFile:        OperatorCredentialsSecretUserNameKey,
		OperatorPasswordFile:        OperatorCredentialsSecretPasswordKey,
		OperatorUserCreatedFilePath: getJenkinsHomePath(jenkins) + "/operatorUserCreated",
	}

	output, err := render.Render(createOperatorUserGroovyFmtTemplate, data)
	if err != nil {
		return nil, err
	}

	return &output, nil
}

// GetInitConfigurationConfigMapName returns name of Kubernetes config map used to init configuration
func GetInitConfigurationConfigMapName(jenkins *v1alpha2.Jenkins) string {
	return fmt.Sprintf("%s-init-configuration-%s", constants.OperatorName, jenkins.ObjectMeta.Name)
}

// NewInitConfigurationConfigMap builds Kubernetes config map used to init configuration
func NewInitConfigurationConfigMap(meta metav1.ObjectMeta, jenkins *v1alpha2.Jenkins) (*corev1.ConfigMap, error) {
	meta.Name = GetInitConfigurationConfigMapName(jenkins)

	createJenkinsOperatorUserGroovy, err := buildCreateJenkinsOperatorUserGroovyScript(jenkins)
	if err != nil {
		return nil, err
	}

	return &corev1.ConfigMap{
		TypeMeta:   buildConfigMapTypeMeta(),
		ObjectMeta: meta,
		Data: map[string]string{
			createOperatorUserFileName: *createJenkinsOperatorUserGroovy,
		},
	}, nil
}
