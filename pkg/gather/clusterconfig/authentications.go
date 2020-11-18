package clusterconfig

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog"

	operatorv1 "github.com/openshift/api/operator/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	"github.com/openshift/client-go/operator/clientset/versioned/scheme"
	operatorv1client "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1"
	_ "k8s.io/apimachinery/pkg/runtime/serializer/yaml"

	"github.com/openshift/insights-operator/pkg/record"
)

// GatherClusterAuthentication fetches the cluster Authentication - the Authentication with name cluster.
//
// The Kubernetes api https://github.com/openshift/client-go/blob/master/config/clientset/versioned/typed/config/v1/authentication.go#L50
// Response see https://docs.openshift.com/container-platform/4.3/rest_api/index.html#authentication-v1operator-openshift-io
//
// Location in archive: config/authentication/
// See: docs/insights-archive-sample/config/authentication
func GatherClusterAuthentication(g *Gatherer) func() ([]record.Record, []error) {
	return func() ([]record.Record, []error) {
		gatherConfigClient, err := configv1client.NewForConfig(g.gatherKubeConfig)
		if err != nil {
			return nil, []error{err}
		}
		gatherOperatorClient, err := operatorv1client.NewForConfig(g.gatherKubeConfig)
		if err != nil {
			return nil, []error{err}
		}
		configRec, confErr := gatherClusterAuthentication(g.ctx, gatherConfigClient)
		operatorRec, operatorErr := gatherClusterAuthenticationResource(g.ctx, gatherOperatorClient)
		return append(configRec, operatorRec...), append(confErr, operatorErr...)
	}
}
func gatherClusterAuthentication(ctx context.Context, configClient configv1client.ConfigV1Interface) ([]record.Record, []error) {
	config, err := configClient.Authentications().Get(ctx, "cluster", metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, []error{err}
	}

	return []record.Record{{Name: "config/authentication", Item: Anonymizer{config}}}, nil
}

func gatherClusterAuthenticationResource(ctx context.Context, operatorClient *operatorv1client.OperatorV1Client) ([]record.Record, []error) {
	auth, err := operatorClient.Authentications().Get(ctx, "cluster", metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, []error{err}
	}
	// TypeMeta is empty - see https://github.com/kubernetes/kubernetes/issues/3030
	kinds, _, err := scheme.Scheme.ObjectKinds(auth)
	if err != nil {
		return nil, []error{err}
	}
	if len(kinds) > 1 {
		klog.Warningf("More kinds for authentication operator resource %s", kinds)
	}
	objKind := kinds[0]
	return []record.Record{{
		Name: fmt.Sprintf("config/clusteroperator/%s/%s/%s", objKind.Group, strings.ToLower(objKind.Kind), auth.Name),
		Item: AuthenticationAnonymizer{auth},
	}}, nil
}

// AuthenticationAnonymizer implements serialization with marshalling
type AuthenticationAnonymizer struct {
	*operatorv1.Authentication
}

// Marshal serializes ImagePruner with anonymization
func (a AuthenticationAnonymizer) Marshal(_ context.Context) ([]byte, error) {
	return runtime.Encode(operatorSerializer.LegacyCodec(operatorv1.SchemeGroupVersion), a.Authentication)
}

// GetExtension returns extension for anonymized image pruner objects
func (a AuthenticationAnonymizer) GetExtension() string {
	return "json"
}
