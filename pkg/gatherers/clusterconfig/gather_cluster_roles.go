package clusterconfig

import (
	"context"
	"fmt"

	"github.com/openshift/insights-operator/pkg/record"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// GatherClusterRoles Collects definition of the "admin" and "edit" cluster roles.
//
// ### API Reference
// - https://github.com/kubernetes/kubernetes/blob/master/pkg/apis/rbac/types.go
//
// ### Sample data
// - docs/insights-archive-sample/cluster-scoped-resources/rbac.authorization.k8s.io/clusterroles
//
// ### Location in archive
// - `cluster-scoped-resources/rbac.authorization.k8s.io/clusterroles/`
//
// ### Config ID
// `clusterconfig/clusterroles`
//
// ### Released version
// - 4.18.0
//
// ### Backported versions
//
// ### Changes
// None
func (g *Gatherer) GatherClusterRoles(ctx context.Context) ([]record.Record, []error) {
	kubeClient, err := kubernetes.NewForConfig(g.gatherProtoKubeConfig)
	if err != nil {
		return nil, []error{err}
	}

	return gatherClusterRoles(ctx, kubeClient, []string{"admin", "edit"})
}

func gatherClusterRoles(ctx context.Context, kubeCli *kubernetes.Clientset, names []string) ([]record.Record, []error) {
	var errs []error
	var records []record.Record
	for _, name := range names {
		clusterRoleRec, err := gatherClusterRole(ctx, name, kubeCli)
		if err != nil {
			errs = append(errs, err)
		} else {
			records = append(records, *clusterRoleRec)
		}
	}
	return records, errs
}

func gatherClusterRole(ctx context.Context, name string, kubeCli *kubernetes.Clientset) (*record.Record, error) {
	clusterRole, err := kubeCli.RbacV1().ClusterRoles().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return &record.Record{
		Name: fmt.Sprintf("cluster-scoped-resources/rbac.authorization.k8s.io/clusterroles/%s", clusterRole.Name),
		Item: record.ResourceMarshaller{Resource: clusterRole},
	}, nil
}
