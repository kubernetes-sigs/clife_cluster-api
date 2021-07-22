# Releasing
<!-- START doctoc generated TOC please keep comment here to allow auto update -->
<!-- DON'T EDIT THIS SECTION, INSTEAD RE-RUN doctoc TO UPDATE -->


- [Prerequisites](#prerequisites)
  - [`gcloud`](#gcloud)
  - [`docker`](#docker)
- [Output](#output)
  - [Expected artifacts](#expected-artifacts)
  - [Artifact locations](#artifact-locations)
- [Process](#process)
  - [Permissions](#permissions)

<!-- END doctoc generated TOC please keep comment here to allow auto update -->

## Prerequisites

### `gcloud`

With gcloud, run `gcloud auth login` and select your account [listed here](https://github.com/kubernetes/k8s.io/blob/05ada8c9ff90e7921e10d86ac5d59f5c1f4f74dc/groups/groups.yaml#L113). Open a PR if your account is not listed there but you believe it should be.

### `docker`

Enable the [experimental features for the docker CLI](https://docs.docker.com/engine/reference/commandline/cli/#environment-variables) by setting the appropriate environment variable.

```
export DOCKER_CLI_EXPERIMENTAL=enabled
```

## Output

### Expected artifacts

1. A container image of the shared cluster-api controller manager
2. A git tag for providers to use

### Artifact locations

1. The container image is found in the registry `k8s.gcr.io/cluster-api/` with an image
   name of `cluster-api-controller` and a tag that matches the release version. For
   example, in the `v0.1.5` release, the container image location is
   `k8s.gcr.io/cluster-api/cluster-api-controller:v0.1.5`

2. Prior to the `v0.1.5` release, the container image is found in the registry
   `gcr.io/k8s-cluster-api` with an image name of `cluster-api-controller` and a tag
   that matches the release version. For example, in the `v0.1.4` release, the container
   image location is `gcr.io/k8s-cluster-api/cluster-api-controller:v0.1.4`

3. Prior to the `v0.1.4` release, the container image is found in the
   registry `gcr.io/k8s-cluster-api` with an image name of `cluster-api-controller`
   and a tag that matches the release version. For example, in the `0.1.3` release,
   the container image location is `gcr.io/k8s-cluster-api/cluster-api-controller:0.1.3`

## Process

For version v0.x.y:

1. Create an annotated tag
   > NOTE: To use your GPG signature when pushing the tag, use `git tag -s [...]` instead)
   - `git tag -a v0.x.y -m v0.x.y`
   - `git tag test/v0.x.y` (:warning: MUST NOT be an annotated tag)
1. Push the tag to the GitHub repository
   > NOTE: `origin` should be the name of the remote pointing to `github.com/kubernetes-sigs/cluster-api`
   - `git push origin v0.x.y`
   - `git push origin test/v0.x.y`
1. Run `make release` to build artifacts (the image is automatically built by CI)
1. Follow the [Image Promotion process](https://git.k8s.io/k8s.io/k8s.gcr.io#image-promoter) to promote the image from the staging repo to `k8s.gcr.io/cluster-api`
1. Create a release in GitHub based on the tag created above
1. Release notes can be created by running `go run ./hack/tools/release/notes.go --from=<PREV_VERSION>`, which will generate an output that can be copied to the drafted release in GitHub.
   Pay close attention to the `## :question: Sort these by hand` section, as it contains items that need to be manually sorted.

### Permissions

Releasing requires a particular set of permissions.

* Push access to the staging gcr bucket
* Tag push access to the GitHub repository
* GitHub Release creation access
