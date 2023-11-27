package resolver

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
	"github.com/go-logr/logr"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kedav1alpha1 "github.com/kedacore/keda/v2/apis/keda/v1alpha1"
)

type AwsSecretManagerHandler struct {
	secretManager *kedav1alpha1.AwsSecretManager
	session       *session.Session
	secretclient  *secretsmanager.SecretsManager
}

func NewAwsSecretManagerHandler(a *kedav1alpha1.AwsSecretManager) *AwsSecretManagerHandler {
	return &AwsSecretManagerHandler{
		secretManager: a,
	}
}

func (ash *AwsSecretManagerHandler) Read(secretName, versionID, versionStage string) (string, error) {
	input := &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(secretName),
	}
	if versionID != "" {
		input.VersionId = aws.String(versionID)
	}
	if versionStage != "" {
		input.VersionStage = aws.String(versionStage)
	}
	result, err := ash.secretclient.GetSecretValue(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			switch aerr.Code() {
			case secretsmanager.ErrCodeResourceNotFoundException:
				err = fmt.Errorf(secretsmanager.ErrCodeResourceNotFoundException + ": " + aerr.Error())
				return "", err
			case secretsmanager.ErrCodeInvalidParameterException:
				err = fmt.Errorf(secretsmanager.ErrCodeInvalidParameterException + ": " + aerr.Error())
				return "", err
			case secretsmanager.ErrCodeInvalidRequestException:
				err = fmt.Errorf(secretsmanager.ErrCodeInvalidRequestException + ": " + aerr.Error())
				return "", err
			case secretsmanager.ErrCodeDecryptionFailure:
				err = fmt.Errorf(secretsmanager.ErrCodeDecryptionFailure + ": " + aerr.Error())
				return "", err
			case secretsmanager.ErrCodeInternalServiceError:
				err = fmt.Errorf(secretsmanager.ErrCodeInternalServiceError + ": " + aerr.Error())
				return "", err
			default:
				err = fmt.Errorf(aerr.Error())
				return "", err
			}
		} else {
			err = fmt.Errorf(err.Error())
			return "", err
		}
	}
	return *result.SecretString, nil
}

func (ash *AwsSecretManagerHandler) Initialize(ctx context.Context, client client.Client, logger logr.Logger, triggerNamespace string, secretsLister corev1listers.SecretLister) error {
	config, err := ash.getcredentials(ctx, client, logger, triggerNamespace, secretsLister)
	if err != nil {
		return err
	}
	if ash.secretManager.Cloud.Region != "" {
		config.WithRegion(ash.secretManager.Cloud.Region)
	}
	if ash.secretManager.Cloud.Endpoint != "" {
		config.WithEndpoint(ash.secretManager.Cloud.Endpoint)
	}
	ash.session = session.Must(session.NewSession())
	ash.secretclient = secretsmanager.New(ash.session, config)
	return err
}

func (ash *AwsSecretManagerHandler) getcredentials(ctx context.Context, client client.Client, logger logr.Logger, triggerNamespace string, secretsLister corev1listers.SecretLister) (*aws.Config, error) {
	config := aws.NewConfig()

	podIdentity := ash.secretManager.PodIdentity
	if podIdentity == nil {
		podIdentity = &kedav1alpha1.AuthPodIdentity{}
	}

	switch podIdentity.Provider {
	case "", kedav1alpha1.PodIdentityProviderNone:
		accessKeyID := resolveAuthSecret(ctx, client, logger, ash.secretManager.Credentials.AccessKey.ValueFrom.SecretKeyRef.Name, triggerNamespace, ash.secretManager.Credentials.AccessKey.ValueFrom.SecretKeyRef.Key, secretsLister)
		accessSecretKey := resolveAuthSecret(ctx, client, logger, ash.secretManager.Credentials.AccessSecretKey.ValueFrom.SecretKeyRef.Name, triggerNamespace, ash.secretManager.Credentials.AccessSecretKey.ValueFrom.SecretKeyRef.Key, secretsLister)
		if accessKeyID == "" || accessSecretKey == "" {
			return nil, fmt.Errorf("AccessKeyID and AccessSecretKey are expected when not using a pod identity provider")
		}
		config.WithCredentials(credentials.NewStaticCredentials(accessKeyID, accessSecretKey, ""))
		return config, nil

	// case kedav1alpha1.PodIdentityProviderAwsEKS:
	// 	awsRoleArn, err := ash.getRoleArnAwsEKS(ctx, client, logger, triggerNamespace, podTemplateSpec)
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// 	config.WithCredentials(stscreds.NewCredentials(ash.session, awsRoleArn))
	// 	return config, nil
	// case kedav1alpha1.PodIdentityProviderAwsKiam:
	// 	awsRoleArn := podTemplateSpec.ObjectMeta.Annotations[kedav1alpha1.PodIdentityAnnotationKiam]
	// 	config.WithCredentials(stscreds.NewCredentials(ash.session, awsRoleArn))
	// 	return config, nil
	default:
		return nil, fmt.Errorf("pod identity provider %s not supported", podIdentity.Provider)
	}
}

// func (ash *AwsSecretManagerHandler) getRoleArnAwsEKS(ctx context.Context, client client.Client, _ logr.Logger, triggerNamespace string, podTemplateSpec *corev1.PodTemplateSpec) (string, error) {
// 	serviceAccountName := podTemplateSpec.Spec.ServiceAccountName
// 	serviceAccount := &corev1.ServiceAccount{}
// 	err := client.Get(ctx, types.NamespacedName{Name: serviceAccountName, Namespace: triggerNamespace}, serviceAccount)
// 	if err != nil {
// 		return "", err
// 	}
// 	return serviceAccount.Annotations[kedav1alpha1.PodIdentityAnnotationEKS], nil
// }
