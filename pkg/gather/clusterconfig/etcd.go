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

// GatherClusterEtcd fetches the Etcd cluster resource configuration
//
// Location in archive: config/clusteroperator/operator.openshift.io/etcd/
func GatherClusterEtcd(g *Gatherer) func() ([]record.Record, []error) {
	return func() ([]record.Record, []error) {
		operatorClient, err := operatorv1client.NewForConfig(g.gatherKubeConfig)
		if err != nil {
			return nil, []error{err}
		}
		return gatherClusterEtcd(g.ctx, operatorClient)
	}
}

func gatherClusterEtcd(ctx context.Context, operatorClient *operatorv1client.OperatorV1Client) ([]record.Record, []error) {
	etcd, err := operatorClient.Etcds().Get(ctx, "cluster", metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, []error{err}
	}
	// TypeMeta is empty - see https://github.com/kubernetes/kubernetes/issues/3030
	kinds, _, err := operatorScheme.ObjectKinds(etcd)
	if err != nil {
		return nil, []error{err}
	}
	if len(kinds) > 1 {
		klog.Warningf("More kinds for Console operator resource %s", kinds)
	}
	objKind := kinds[0]
	return []record.Record{{
		Name: fmt.Sprintf("config/clusteroperator/%s/%s/%s", objKind.Group, strings.ToLower(objKind.Kind), etcd.Name),
		Item: EtcdAnonymizer{etcd},
	}}, nil
}

// EtcdAnonymizer implements serialization with marshalling
type EtcdAnonymizer struct {
	*operatorv1.Etcd
}

// Marshal serializes Etcd with anonymization
func (a EtcdAnonymizer) Marshal(_ context.Context) ([]byte, error) {
	return runtime.Encode(operatorSerializer.LegacyCodec(operatorv1.SchemeGroupVersion), a.Etcd)
}

// GetExtension returns extension for anonymized Etcd objects
func (a EtcdAnonymizer) GetExtension() string {
	return "json"
}
