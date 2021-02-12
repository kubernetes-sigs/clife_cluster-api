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

package internal

import (
	"context"

	"github.com/pkg/errors"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/controlplane/kubeadm/internal/etcd"
	etcdutil "sigs.k8s.io/cluster-api/controlplane/kubeadm/internal/etcd/util"
)

type etcdClientFor interface {
	forFirstAvailableNode(ctx context.Context, nodeNames []string) (*etcd.Client, error)
	forLeader(ctx context.Context, nodeNames []string) (*etcd.Client, error)
}

// ReconcileEtcdMembers iterates over all etcd members and finds members that do not have corresponding nodes.
// If there are any such members, it deletes them from etcd and removes their nodes from the kubeadm configmap so that kubeadm does not run etcd health checks on them.
func (w *Workload) ReconcileEtcdMembers(ctx context.Context, nodeNames []string) ([]string, error) {
	removedMembers := []string{}
	errs := []error{}
	for _, nodeName := range nodeNames {
		// Create the etcd Client for the etcd Pod scheduled on the Node
		etcdClient, err := w.etcdClientGenerator.forFirstAvailableNode(ctx, []string{nodeName})
		if err != nil {
			continue
		}
		defer etcdClient.Close()

		members, err := etcdClient.Members(ctx)
		if err != nil {
			continue
		}

		// Check if any member's node is missing from workload cluster
		// If any, delete it with best effort
	loopmembers:
		for _, member := range members {
			// If this member is just added, it has a empty name until the etcd pod starts. Ignore it.
			if member.Name == "" {
				continue
			}

			for _, nodeName := range nodeNames {
				if member.Name == nodeName {
					// We found the matching node, continue with the outer loop.
					continue loopmembers
				}
			}

			// If we're here, the node cannot be found.
			removedMembers = append(removedMembers, member.Name)
			if err := w.removeMemberForNode(ctx, member.Name); err != nil {
				errs = append(errs, err)
			}

			if err := w.RemoveNodeFromKubeadmConfigMap(ctx, member.Name); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return removedMembers, kerrors.NewAggregate(errs)
}

// RemoveEtcdMemberForMachine removes the etcd member from the target cluster's etcd cluster.
// Removing the last remaining member of the cluster is not supported.
func (w *Workload) RemoveEtcdMemberForMachine(ctx context.Context, machine *clusterv1.Machine) error {
	if machine == nil || machine.Status.NodeRef == nil {
		// Nothing to do, no node for Machine
		return nil
	}
	return w.removeMemberForNode(ctx, machine.Status.NodeRef.Name)
}

func (w *Workload) removeMemberForNode(ctx context.Context, name string) error {
	controlPlaneNodes, err := w.getControlPlaneNodes(ctx)
	if err != nil {
		return err
	}
	if len(controlPlaneNodes.Items) < 2 {
		return ErrControlPlaneMinNodes
	}

	// Exclude node being removed from etcd client node list
	var remainingNodes []string
	for _, n := range controlPlaneNodes.Items {
		if n.Name != name {
			remainingNodes = append(remainingNodes, n.Name)
		}
	}
	etcdClient, err := w.etcdClientGenerator.forFirstAvailableNode(ctx, remainingNodes)
	if err != nil {
		return errors.Wrap(err, "failed to create etcd client")
	}
	defer etcdClient.Close()

	// List etcd members. This checks that the member is healthy, because the request goes through consensus.
	members, err := etcdClient.Members(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to list etcd members using etcd client")
	}
	member := etcdutil.MemberForName(members, name)

	// The member has already been removed, return immediately
	if member == nil {
		return nil
	}

	if err := etcdClient.RemoveMember(ctx, member.ID); err != nil {
		return errors.Wrap(err, "failed to remove member from etcd")
	}

	return nil
}

// ForwardEtcdLeadership forwards etcd leadership to the first follower
func (w *Workload) ForwardEtcdLeadership(ctx context.Context, machine *clusterv1.Machine, leaderCandidate *clusterv1.Machine) error {
	if machine == nil || machine.Status.NodeRef == nil {
		return nil
	}
	if leaderCandidate == nil {
		return errors.New("leader candidate cannot be nil")
	}
	if leaderCandidate.Status.NodeRef == nil {
		return errors.New("leader has no node reference")
	}

	nodes, err := w.getControlPlaneNodes(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to list control plane nodes")
	}
	nodeNames := make([]string, 0, len(nodes.Items))
	for _, node := range nodes.Items {
		nodeNames = append(nodeNames, node.Name)
	}
	etcdClient, err := w.etcdClientGenerator.forLeader(ctx, nodeNames)
	if err != nil {
		return errors.Wrap(err, "failed to create etcd client")
	}
	defer etcdClient.Close()

	members, err := etcdClient.Members(ctx)
	if err != nil {
		return errors.Wrap(err, "failed to list etcd members using etcd client")
	}

	currentMember := etcdutil.MemberForName(members, machine.Status.NodeRef.Name)
	if currentMember == nil || currentMember.ID != etcdClient.LeaderID {
		// nothing to do, this is not the etcd leader
		return nil
	}

	// Move the leader to the provided candidate.
	nextLeader := etcdutil.MemberForName(members, leaderCandidate.Status.NodeRef.Name)
	if nextLeader == nil {
		return errors.Errorf("failed to get etcd member from node %q", leaderCandidate.Status.NodeRef.Name)
	}
	if err := etcdClient.MoveLeader(ctx, nextLeader.ID); err != nil {
		return errors.Wrapf(err, "failed to move leader")
	}
	return nil
}

type EtcdMemberStatus struct {
	Name       string
	Responsive bool
}

// EtcdStatus returns the current status of the etcd cluster
// NOTE: This methods uses control plane machines/nodes only to get in contact with etcd,
// but then it relies on etcd as ultimate source of truth for the list of members.
// This is intended to allow informed decisions on actions impacting etcd quorum.
func (w *Workload) EtcdMembers(ctx context.Context) ([]string, error) {
	nodes, err := w.getControlPlaneNodes(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list control plane nodes")
	}
	nodeNames := make([]string, 0, len(nodes.Items))
	for _, node := range nodes.Items {
		nodeNames = append(nodeNames, node.Name)
	}
	etcdClient, err := w.etcdClientGenerator.forLeader(ctx, nodeNames)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create etcd client")
	}
	defer etcdClient.Close()

	members, err := etcdClient.Members(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list etcd members using etcd client")
	}

	names := []string{}
	for _, member := range members {
		names = append(names, member.Name)
	}
	return names, nil
}
