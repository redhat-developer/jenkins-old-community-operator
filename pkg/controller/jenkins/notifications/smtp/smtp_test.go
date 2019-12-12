package smtp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime/quotedprintable"
	"net"
	"regexp"
	"testing"
	"time"

	"github.com/redhat-developer/jenkins-operator/pkg/apis/jenkins/v1alpha2"
	"github.com/redhat-developer/jenkins-operator/pkg/controller/jenkins/notifications/event"
	"github.com/redhat-developer/jenkins-operator/pkg/controller/jenkins/notifications/reason"

	"github.com/emersion/go-smtp"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testSMTPUsername = "username"
	testSMTPPassword = "password"

	testSMTPPort = 1025

	testFrom    = "test@localhost"
	testTo      = "test.to@localhost"
	testSubject = "Jenkins Operator Notification"

	// Headers titles
	fromHeader    = "From"
	toHeader      = "To"
	subjectHeader = "Subject"
)

var (
	testPhase     = event.PhaseUser
	testCrName    = "test-cr"
	testNamespace = "default"
	testReason    = reason.NewPodRestart(
		reason.KubernetesSource,
		[]string{"test-reason-1"},
		[]string{"test-verbose-1"}...,
	)
	testLevel = v1alpha2.NotificationLevelWarning
)

type testServer struct {
	event event.Event
}

// Login handles a login command with username and password.
func (bkd *testServer) Login(state *smtp.ConnectionState, username, password string) (smtp.Session, error) {
	if username != testSMTPUsername || password != testSMTPPassword {
		return nil, errors.New("invalid username or password")
	}
	return &testSession{event: bkd.event}, nil
}

// AnonymousLogin requires clients to authenticate using SMTP AUTH before sending emails
func (bkd *testServer) AnonymousLogin(state *smtp.ConnectionState) (smtp.Session, error) {
	return nil, smtp.ErrAuthRequired
}

// A Session is returned after successful login.
type testSession struct {
	event event.Event
}

func (s *testSession) Mail(from string) error {
	if from != testFrom {
		return fmt.Errorf("`From` header is not equal: '%s', expected '%s'", from, testFrom)
	}
	return nil
}

func (s *testSession) Rcpt(to string) error {
	if to != testTo {
		return fmt.Errorf("`To` header is not equal: '%s', expected '%s'", to, testTo)
	}
	return nil
}

func (s *testSession) Data(r io.Reader) error {
	contentRegex := regexp.MustCompile(`\t+<tr>\n\t+<td><b>(.*):</b></td>\n\t+<td>(.*)</td>\n\t+</tr>`)
	headersRegex := regexp.MustCompile(`(.*):\s(.*)`)

	b, err := ioutil.ReadAll(quotedprintable.NewReader(r))
	if err != nil {
		return err
	}

	content := contentRegex.FindAllStringSubmatch(string(b), -1)
	headers := headersRegex.FindAllStringSubmatch(string(b), -1)

	if s.event.Jenkins.Name == content[0][1] {
		return fmt.Errorf("jenkins CR not identical: %s, expected: %s", content[0][1], s.event.Jenkins.Name)
	} else if string(s.event.Phase) == content[1][1] {
		return fmt.Errorf("phase not identical: %s, expected: %s", content[1][1], s.event.Phase)
	}

	for i := range headers {
		if headers[i][1] == fromHeader && headers[i][2] != testFrom {
			return fmt.Errorf("`From` header is not equal: '%s', expected '%s'", headers[i][2], testFrom)
		} else if headers[i][1] == toHeader && headers[i][2] != testTo {
			return fmt.Errorf("`To` header is not equal: '%s', expected '%s'", headers[i][2], testTo)
		} else if headers[i][1] == subjectHeader && headers[i][2] != testSubject {
			return fmt.Errorf("`Subject` header is not equal: '%s', expected '%s'", headers[i][2], testSubject)
		}
	}

	return nil
}

func (s *testSession) Reset() {}

func (s *testSession) Logout() error {
	return nil
}

func TestSMTP_Send(t *testing.T) {
	event := event.Event{
		Jenkins: v1alpha2.Jenkins{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testCrName,
				Namespace: testNamespace,
			},
		},
		Phase:  testPhase,
		Level:  testLevel,
		Reason: testReason,
	}

	fakeClient := fake.NewFakeClient()
	testUsernameSelectorKeyName := "test-username-selector"
	testPasswordSelectorKeyName := "test-password-selector"
	testSecretName := "test-secret"

	smtpClient := SMTP{k8sClient: fakeClient, config: v1alpha2.Notification{
		SMTP: &v1alpha2.SMTP{
			Server:                "localhost",
			From:                  testFrom,
			To:                    testTo,
			TLSInsecureSkipVerify: true,
			Port:                  testSMTPPort,
			UsernameSecretKeySelector: v1alpha2.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: testSecretName,
				},
				Key: testUsernameSelectorKeyName,
			},
			PasswordSecretKeySelector: v1alpha2.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: testSecretName,
				},
				Key: testPasswordSelectorKeyName,
			},
		},
	}}

	ts := &testServer{event: event}

	// Create fake SMTP server

	s := smtp.NewServer(ts)

	s.Addr = fmt.Sprintf(":%d", testSMTPPort)
	s.Domain = "localhost"
	s.ReadTimeout = 10 * time.Second
	s.WriteTimeout = 10 * time.Second
	s.MaxMessageBytes = 1024 * 1024
	s.MaxRecipients = 50
	s.AllowInsecureAuth = true

	// Create secrets
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testSecretName,
			Namespace: testNamespace,
		},

		Data: map[string][]byte{
			testUsernameSelectorKeyName: []byte(testSMTPUsername),
			testPasswordSelectorKeyName: []byte(testSMTPPassword),
		},
	}

	err := fakeClient.Create(context.TODO(), secret)
	assert.NoError(t, err)

	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", testSMTPPort))
	assert.NoError(t, err)

	go func() {
		err := s.Serve(l)
		assert.NoError(t, err)
	}()

	err = smtpClient.Send(event)

	assert.NoError(t, err)
}

func TestGenerateMessage(t *testing.T) {
	t.Run("happy", func(t *testing.T) {
		crName := "test-jenkins"
		phase := event.PhaseBase
		level := v1alpha2.NotificationLevelInfo
		res := reason.NewUndefined(reason.KubernetesSource, []string{"test"}, []string{"test-verbose"}...)

		from := "from@jenkins.local"
		to := "to@jenkins.local"

		e := event.Event{
			Jenkins: v1alpha2.Jenkins{
				ObjectMeta: metav1.ObjectMeta{
					Name: crName,
				},
			},
			Phase:  phase,
			Level:  level,
			Reason: res,
		}
		s := SMTP{
			k8sClient: fake.NewFakeClient(),
			config: v1alpha2.Notification{
				LoggingLevel: level,
				SMTP: &v1alpha2.SMTP{
					From: from,
					To:   to,
				},
			},
		}
		message := s.generateMessage(e)
		assert.NotNil(t, message)
	})

	t.Run("with nils", func(t *testing.T) {
		crName := "nil"
		phase := event.PhaseBase
		level := v1alpha2.NotificationLevelInfo
		res := reason.NewUndefined(reason.KubernetesSource, []string{"nil"}, []string{"nil"}...)

		from := "nil"
		to := "nil"

		e := event.Event{
			Jenkins: v1alpha2.Jenkins{
				ObjectMeta: metav1.ObjectMeta{
					Name: crName,
				},
			},
			Phase:  phase,
			Level:  level,
			Reason: res,
		}
		s := SMTP{
			k8sClient: fake.NewFakeClient(),
			config: v1alpha2.Notification{
				LoggingLevel: level,
				SMTP: &v1alpha2.SMTP{
					From: from,
					To:   to,
				},
			},
		}
		message := s.generateMessage(e)
		assert.NotNil(t, message)
	})

	t.Run("with empty strings", func(t *testing.T) {
		crName := ""
		phase := event.PhaseBase
		level := v1alpha2.NotificationLevelInfo
		res := reason.NewUndefined(reason.KubernetesSource, []string{""}, []string{""}...)

		from := ""
		to := ""

		e := event.Event{
			Jenkins: v1alpha2.Jenkins{
				ObjectMeta: metav1.ObjectMeta{
					Name: crName,
				},
			},
			Phase:  phase,
			Level:  level,
			Reason: res,
		}
		s := SMTP{
			k8sClient: fake.NewFakeClient(),
			config: v1alpha2.Notification{
				LoggingLevel: level,
				SMTP: &v1alpha2.SMTP{
					From: from,
					To:   to,
				},
			},
		}
		message := s.generateMessage(e)
		assert.NotNil(t, message)
	})
}
