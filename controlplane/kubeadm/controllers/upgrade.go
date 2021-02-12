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

package controllers

import (
	"context"

	"github.com/blang/semver"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1alpha4"
	"sigs.k8s.io/cluster-api/controlplane/kubeadm/internal"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *KubeadmControlPlaneReconciler) upgradeControlPlane(
	ctx context.Context,
	cluster *clusterv1.Cluster,
	kcp *controlplanev1.KubeadmControlPlane,
	controlPlane *internal.ControlPlane,
	machinesRequireUpgrade internal.FilterableMachineCollection,
) (ctrl.Result, error) {
	logger := controlPlane.Logger()

	// TODO: handle reconciliation of etcd members and kubeadm config in case they get out of sync with cluster

	workloadCluster, err := r.managementCluster.GetWorkloadCluster(ctx, util.ObjectKey(cluster))
	if err != nil {
		logger.Error(err, "failed to get remote client for workload cluster", "cluster key", util.ObjectKey(cluster))
		return ctrl.Result{}, err
	}

	parsedVersion, err := semver.ParseTolerant(kcp.Spec.Version)
	if err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "failed to parse kubernetes version %q", kcp.Spec.Version)
	}

	if err := workloadCluster.ReconcileKubeletRBACRole(ctx, parsedVersion); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to reconcile the remote kubelet RBAC role")
	}

	if err := workloadCluster.ReconcileKubeletRBACBinding(ctx, parsedVersion); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to reconcile the remote kubelet RBAC binding")
	}

	// Ensure kubeadm cluster role  & bindings for v1.18+
	// as per https://github.com/kubernetes/kubernetes/commit/b117a928a6c3f650931bdac02a41fca6680548c4
	if err := workloadCluster.AllowBootstrapTokensToGetNodes(ctx); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to set role and role binding for kubeadm")
	}

	configMapKey := ctrlclient.ObjectKey{Name: "kubeadm-config", Namespace: metav1.NamespaceSystem}
	kubeadmConfig, err := workloadCluster.GetKubeadmConfig(ctx, configMapKey)
	if err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "failed to fetch kubeadm config map")
	}

	// Initialize the patch helper.
	patchHelper, err := patch.NewHelper(kubeadmConfig.GetConfigMap(), workloadCluster.GetClient())
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := kubeadmConfig.ReconcileKubernetesVersion(parsedVersion); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to update the kubernetes version in the kubeadm config map")
	}

	if kcp.Spec.KubeadmConfigSpec.ClusterConfiguration != nil {
		imageRepository := kcp.Spec.KubeadmConfigSpec.ClusterConfiguration.ImageRepository
		if err := kubeadmConfig.ReconcileImageRepository(imageRepository); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to update the image repository in the kubeadm config map")
		}
	}

	if kcp.Spec.KubeadmConfigSpec.ClusterConfiguration != nil && kcp.Spec.KubeadmConfigSpec.ClusterConfiguration.Etcd.Local != nil {
		meta := kcp.Spec.KubeadmConfigSpec.ClusterConfiguration.Etcd.Local.ImageMeta
		if err := kubeadmConfig.ReconcileEtcdImageMeta(meta); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to update the etcd version in the kubeadm config map")
		}
	}

	if kcp.Spec.KubeadmConfigSpec.ClusterConfiguration != nil {
		if err := kubeadmConfig.ReconcileAPIServer(kcp.Spec.KubeadmConfigSpec.ClusterConfiguration.APIServer); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to update api server in the kubeadm config map")
		}

		if err := kubeadmConfig.ReconcileControllerManager(kcp.Spec.KubeadmConfigSpec.ClusterConfiguration.ControllerManager); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to update controller manager in the kubeadm config map")
		}

		if err := kubeadmConfig.ReconcileScheduler(kcp.Spec.KubeadmConfigSpec.ClusterConfiguration.Scheduler); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to update scheduler in the kubeadm config map")
		}
	}

	if err := patchHelper.Patch(ctx, kubeadmConfig.GetConfigMap()); err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "unable to patch kubeadm config map")
	}

	if err := workloadCluster.UpdateKubeletConfigMap(ctx, parsedVersion); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to upgrade kubelet config map")
	}

	status, err := workloadCluster.ClusterStatus(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	if status.Nodes <= *kcp.Spec.Replicas {
		// scaleUp ensures that we don't continue scaling up while waiting for Machines to have NodeRefs
		return r.scaleUpControlPlane(ctx, cluster, kcp, controlPlane)
	}
	return r.scaleDownControlPlane(ctx, cluster, kcp, controlPlane, machinesRequireUpgrade)
}
