# Kubetest2 Cluster API Deployer

This component of [kubetest2](https://github.com/kubernetes-sigs/kubetest2) is
responsible for test cluster lifecycles for clusters deployed with Cluster API.

Users can use this project along with kubetest2 testers to run [Kubernetes
e2e tests](https://github.com/kubernetes/kubernetes/tree/master/test/e2e)
against CAPI-backed clusters.

## Installation
```
GO111MODULE=on go get sigs.k8s.io/cluster-api/test/kubetest2-capi@latest
```

## Usage

As an example, to deploy with CAPD:

```
export KIND_EXPERIMENTAL_DOCKER_NETWORK=bridge
kubetest2 capi --provider docker --kubernetes-version 1.19.1 --up --down
```

Currently this provider does not fully support the kubetest2 `--build` feature.
It is possible to build and deploy only with the [CAPD
provider](../infrastructure/docker), since CAPD takes advantage of the node
images that Kind builds. As an example of how this can be done:

```bash
kubeRoot=$HOME/workspace/kubernetes

pushd $kubeRoot
export KUBE_GIT_VERSION=$(hack/print-workspace-status.sh | grep gitVersion | cut -d' ' -f2 | cut -d'+' -f1)
export KUBERNETES_VERSION=$KUBE_GIT_VERSION
popd
export KIND_EXPERIMENTAL_DOCKER_NETWORK=bridge
cat <<EOF | kubetest2 capi \
  --provider docker \
  --flavor development \
  --up \
  --kube-root $kubeRoot \
  --build \
  --image-name "kindest/node:$KUBE_GIT_VERSION" \
  --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraMounts:
    - hostPath: /var/run/docker.sock
      containerPath: /var/run/docker.sock
EOF
```
