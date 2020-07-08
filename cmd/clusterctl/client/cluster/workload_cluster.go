/*
Copyright 2020 The Kubernetes Authors.

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

package cluster

import (
	"fmt"

	kc "sigs.k8s.io/cluster-api/util/kubeconfig"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// WorkloadCluster has methods for fetching kubeconfig of workload cluster from management cluster.
type WorkloadCluster interface {
	//Get workload cluster kubeconfig
	GetKubeconfig(name string) error
}

// workloadCluster implements WorkloadCluster.
type workloadCluster struct {
	proxy Proxy
}

func (p *workloadCluster) GetKubeconfig(name string) error {
	cs, err := p.proxy.NewClient()
	if err != nil {
		return err
	}

	obj := client.ObjectKey{
		Namespace: "default",
		Name:      name,
	}
	dataBytes, err := kc.FromSecret(ctx, cs, obj)

	data := string(dataBytes)
	fmt.Println(data)
	return err
}

// newWorkloadCluster returns a workloadCluster.
func newWorkloadCluster(proxy Proxy) *workloadCluster {
	return &workloadCluster{
		proxy: proxy,
	}
}
