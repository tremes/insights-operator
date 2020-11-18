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
	operatorv1client "github.com/openshift/client-go/operator/clientset/versioned/typed/operator/v1"
	_ "k8s.io/apimachinery/pkg/runtime/serializer/yaml"

	"github.com/openshift/insights-operator/pkg/record"
)

// GatherClusterConsole fetches the Console cluster resource configuration
//
// Location in archive: config/clusteroperator/operator.openshift.io/console/
func GatherClusterConsole(g *Gatherer) func() ([]record.Record, []error) {
	return func() ([]record.Record, []error) {
		operatorClient, err := operatorv1client.NewForConfig(g.gatherKubeConfig)
		if err != nil {
			return nil, []error{err}
		}
		return gatherClusterConsole(g.ctx, operatorClient)
	}
}

func gatherClusterConsole(ctx context.Context, operatorClient *operatorv1client.OperatorV1Client) ([]record.Record, []error) {
	console, err := operatorClient.Consoles().Get(ctx, "cluster", metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, []error{err}
	}
	// TypeMeta is empty - see https://github.com/kubernetes/kubernetes/issues/3030
	kinds, _, err := operatorScheme.ObjectKinds(console)
	if err != nil {
		return nil, []error{err}
	}
	if len(kinds) > 1 {
		klog.Warningf("More kinds for Console operator resource %s", kinds)
	}
	objKind := kinds[0]
	return []record.Record{{
		Name: fmt.Sprintf("config/clusteroperator/%s/%s/%s", objKind.Group, strings.ToLower(objKind.Kind), console.Name),
		Item: ConsoleAnonymizer{console},
	}}, nil
}

// ConsoleAnonymizer implements serialization with marshalling
type ConsoleAnonymizer struct {
	*operatorv1.Console
}

// Marshal serializes Console with anonymization
func (a ConsoleAnonymizer) Marshal(_ context.Context) ([]byte, error) {
	return runtime.Encode(operatorSerializer.LegacyCodec(operatorv1.SchemeGroupVersion), a.Console)
}

// GetExtension returns extension for anonymized Console objects
func (a ConsoleAnonymizer) GetExtension() string {
	return "json"
}
