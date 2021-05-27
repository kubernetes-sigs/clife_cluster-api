/*
Copyright 2019 The Kubernetes Authors.

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

package cmd

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/client"
)

type moveOptions struct {
	fromKubeconfig        string
	fromKubeconfigContext string
	toKubeconfig          string
	toKubeconfigContext   string
	namespace             string
	dryRun                bool
	cluster               string
}

var mo = &moveOptions{}

var moveCmd = &cobra.Command{
	Use:   "move",
	Short: "Move Cluster API objects and all dependencies between management clusters.",
	Long: LongDesc(`
		Move Cluster API objects and all dependencies between management clusters.

		Note: The destination cluster MUST have the required provider components installed.`),

	Example: Examples(`
		Move Cluster API objects and all dependencies between management clusters.
		clusterctl move --to-kubeconfig=target-kubeconfig.yaml`),
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runMove()
	},
}

func init() {
	moveCmd.Flags().StringVar(&mo.fromKubeconfig, "kubeconfig", "",
		"Path to the kubeconfig file for the source management cluster. If unspecified, default discovery rules apply.")
	moveCmd.Flags().StringVar(&mo.toKubeconfig, "to-kubeconfig", "",
		"Path to the kubeconfig file to use for the destination management cluster.")
	moveCmd.Flags().StringVar(&mo.fromKubeconfigContext, "kubeconfig-context", "",
		"Context to be used within the kubeconfig file for the source management cluster. If empty, current context will be used.")
	moveCmd.Flags().StringVar(&mo.toKubeconfigContext, "to-kubeconfig-context", "",
		"Context to be used within the kubeconfig file for the destination management cluster. If empty, current context will be used.")
	moveCmd.Flags().StringVarP(&mo.namespace, "namespace", "n", "",
		"The namespace where the workload cluster is hosted. If unspecified, the current context's namespace is used.")
	moveCmd.Flags().BoolVar(&mo.dryRun, "dry-run", false,
		"Enable dry run, don't really perform the move actions")
	moveCmd.Flags().StringVarP(&mo.cluster, "cluster", "c", "",
		"The cluster name to move. If not specified, moves all clusters in the namespace, falling back to default behavior.")

	RootCmd.AddCommand(moveCmd)
}

func runMove() error {
	// if no to kubeconfig provided and it's not a dry run, return error
	if mo.toKubeconfig == "" && !mo.dryRun {
		return errors.New("please specify a target cluster using the --to-kubeconfig flag")
	}

	c, err := client.New(cfgFile)
	if err != nil {
		return err
	}

	if err := c.Move(client.MoveOptions{
		FromKubeconfig: client.Kubeconfig{Path: mo.fromKubeconfig, Context: mo.fromKubeconfigContext},
		ToKubeconfig:   client.Kubeconfig{Path: mo.toKubeconfig, Context: mo.toKubeconfigContext},
		Namespace:      mo.namespace,
		DryRun:         mo.dryRun,
		Cluster:        mo.cluster,
	}); err != nil {
		return err
	}
	return nil
}
