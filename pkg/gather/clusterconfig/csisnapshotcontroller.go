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

// GatherClusterCSISnapshotController fetches the CSISnapshotController cluster resource configuration
//
// Location in archive: config/clusteroperator/operator.openshift.io/csisnapshotcontroller/
func GatherClusterCSISnapshotController(g *Gatherer) func() ([]record.Record, []error) {
	return func() ([]record.Record, []error) {
		operatorClient, err := operatorv1client.NewForConfig(g.gatherKubeConfig)
		if err != nil {
			return nil, []error{err}
		}
		return gatherClusterCSISnapshotController(g.ctx, operatorClient)
	}
}

func gatherClusterCSISnapshotController(ctx context.Context, operatorClient *operatorv1client.OperatorV1Client) ([]record.Record, []error) {
	csiSnapshotCtr, err := operatorClient.CSISnapshotControllers().Get(ctx, "cluster", metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, []error{err}
	}
	// TypeMeta is empty - see https://github.com/kubernetes/kubernetes/issues/3030
	kinds, _, err := operatorScheme.ObjectKinds(csiSnapshotCtr)
	if err != nil {
		return nil, []error{err}
	}
	if len(kinds) > 1 {
		klog.Warningf("More kinds for Console operator resource %s", kinds)
	}
	objKind := kinds[0]
	return []record.Record{{
		Name: fmt.Sprintf("config/clusteroperator/%s/%s/%s", objKind.Group, strings.ToLower(objKind.Kind), csiSnapshotCtr.Name),
		Item: CSISnapshotControllerAnonymizer{csiSnapshotCtr},
	}}, nil
}

// CSISnapshotControllerAnonymizer implements serialization with marshalling
type CSISnapshotControllerAnonymizer struct {
	*operatorv1.CSISnapshotController
}

// Marshal serializes CSISnapshotController with anonymization
func (a CSISnapshotControllerAnonymizer) Marshal(_ context.Context) ([]byte, error) {
	return runtime.Encode(operatorSerializer.LegacyCodec(operatorv1.SchemeGroupVersion), a.CSISnapshotController)
}

// GetExtension returns extension for anonymized CSISnapshotController objects
func (a CSISnapshotControllerAnonymizer) GetExtension() string {
	return "json"
}
