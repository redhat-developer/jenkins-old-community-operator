package user

import (
	"strings"

	jenkinsclient "github.com/redhat-developer/jenkins-operator/pkg/controller/jenkins/client"
	"github.com/redhat-developer/jenkins-operator/pkg/controller/jenkins/configuration"
	"github.com/redhat-developer/jenkins-operator/pkg/controller/jenkins/configuration/backuprestore"
	"github.com/redhat-developer/jenkins-operator/pkg/controller/jenkins/configuration/base/resources"
	"github.com/redhat-developer/jenkins-operator/pkg/controller/jenkins/configuration/user/casc"
	"github.com/redhat-developer/jenkins-operator/pkg/controller/jenkins/configuration/user/seedjobs"
	"github.com/redhat-developer/jenkins-operator/pkg/controller/jenkins/groovy"

	"github.com/go-logr/logr"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ReconcileUserConfiguration defines values required for Jenkins user configuration
type ReconcileUserConfiguration struct {
	configuration.Configuration
	jenkinsClient jenkinsclient.Jenkins
	logger        logr.Logger
	config        rest.Config
}

// New create structure which takes care of user configuration
func New(configuration configuration.Configuration, jenkinsClient jenkinsclient.Jenkins, logger logr.Logger, config rest.Config) *ReconcileUserConfiguration {
	return &ReconcileUserConfiguration{
		Configuration: configuration,
		jenkinsClient: jenkinsClient,
		logger:        logger,
		config:        config,
	}
}

// Reconcile it's a main reconciliation loop for user supplied configuration
func (r *ReconcileUserConfiguration) Reconcile() (reconcile.Result, error) {
	backupAndRestore := backuprestore.New(r.Client, r.ClientSet, r.logger, r.Configuration.Jenkins, r.config)

	result, err := r.ensureSeedJobs()
	if err != nil {
		return reconcile.Result{}, err
	}
	if result.Requeue {
		return result, nil
	}

	if err := backupAndRestore.Restore(r.jenkinsClient); err != nil {
		return reconcile.Result{}, err
	}

	result, err = r.ensureUserConfiguration(r.jenkinsClient)
	if err != nil {
		return reconcile.Result{}, err
	}
	if result.Requeue {
		return result, nil
	}

	if err := backupAndRestore.Backup(); err != nil {
		return reconcile.Result{}, err
	}
	if err := backupAndRestore.EnsureBackupTrigger(); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileUserConfiguration) ensureSeedJobs() (reconcile.Result, error) {
	seedJobs := seedjobs.New(r.jenkinsClient, r.Configuration, r.logger)
	done, err := seedJobs.EnsureSeedJobs(r.Configuration.Jenkins)
	if err != nil {
		return reconcile.Result{}, err
	}
	if !done {
		return reconcile.Result{Requeue: true}, nil
	}
	return reconcile.Result{}, nil
}

func (r *ReconcileUserConfiguration) ensureUserConfiguration(jenkinsClient jenkinsclient.Jenkins) (reconcile.Result, error) {
	groovyClient := groovy.New(jenkinsClient, r.Client, r.logger, r.Configuration.Jenkins, "user-groovy", r.Configuration.Jenkins.Spec.GroovyScripts.Customization)

	requeue, err := groovyClient.WaitForSecretSynchronization(resources.GroovyScriptsSecretVolumePath)
	if err != nil {
		return reconcile.Result{}, err
	}
	if requeue {
		return reconcile.Result{Requeue: true}, nil
	}
	requeue, err = groovyClient.Ensure(func(name string) bool {
		return strings.HasSuffix(name, ".groovy")
	}, groovy.AddSecretsLoaderToGroovyScript(resources.GroovyScriptsSecretVolumePath))
	if err != nil {
		return reconcile.Result{}, err
	}
	if requeue {
		return reconcile.Result{Requeue: true}, nil
	}

	configurationAsCodeClient := casc.New(jenkinsClient, r.Client, r.logger, r.Configuration.Jenkins)
	requeue, err = configurationAsCodeClient.Ensure(r.Configuration.Jenkins)
	if err != nil {
		return reconcile.Result{}, err
	}
	if requeue {
		return reconcile.Result{Requeue: true}, nil
	}

	return reconcile.Result{}, nil
}
