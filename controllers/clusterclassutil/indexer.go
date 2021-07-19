/*
Copyright 2021 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package clusterclassutil implements ClusterClass utils
package clusterclassutil

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AddClusterClassNameIndex adds the cluster topology class index to the
// manager cache.
func AddClusterClassNameIndex(ctx context.Context, mgr ctrl.Manager) error {
	if err := mgr.GetCache().IndexField(ctx, &clusterv1.Cluster{},
		clusterv1.ClusterClassNameIndex,
		indexClusterByClassName,
	); err != nil {
		return errors.Wrap(err, "error setting index fields")
	}
	return nil
}

func indexClusterByClassName(o client.Object) []string {
	cluster, ok := o.(*clusterv1.Cluster)
	if !ok {
		panic(fmt.Sprintf("Expected Cluster but got a %T", o))
	}
	if cluster.Spec.Topology != nil {
		return []string{cluster.Spec.Topology.Class}
	}
	return nil
}
