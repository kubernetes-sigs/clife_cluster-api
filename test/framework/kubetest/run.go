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

// Package kubetest implmements kubetest functionality.
package kubetest

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"path"
	"runtime"
	"strconv"
	"strings"

	"github.com/onsi/ginkgo"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/ginkgoextensions"
	"sigs.k8s.io/cluster-api/test/infrastructure/container"
	"sigs.k8s.io/yaml"
)

const (
	standardImage   = "k8s.gcr.io/conformance"
	ciArtifactImage = "gcr.io/k8s-staging-ci-images/conformance"
)

// Export Ginkgo constants.
const (
	DefaultGinkgoNodes            = 1
	DefaultGinkoSlowSpecThreshold = 120
)

type RunInput struct {
	// ClusterProxy is a clusterctl test framework proxy for the workload cluster
	// for which to run kubetest against
	ClusterProxy framework.ClusterProxy
	// NumberOfNodes is the number of cluster nodes that exist for kubetest
	// to be aware of
	NumberOfNodes int
	// ArtifactsDirectory is where conformance suite output will go
	ArtifactsDirectory string
	// Path to the kubetest e2e config file
	ConfigFilePath string
	// GinkgoNodes is the number of Ginkgo nodes to use
	GinkgoNodes int
	// GinkgoSlowSpecThreshold is time in s before spec is marked as slow
	GinkgoSlowSpecThreshold int
	// KubernetesVersion is the version of Kubernetes to test (if not specified, then an attempt to discover the server version is made)
	KubernetesVersion string
	// ConformanceImage is an optional field to specify an exact conformance image
	ConformanceImage string
	// KubeTestRepoListPath is optional file for specifying custom image repositories
	// https://github.com/kubernetes/kubernetes/blob/master/test/images/README.md#testing-the-new-image
	KubeTestRepoListPath string
	// ExtraMounts is a list of extra files or directories to mount.
	ExtraMounts map[string]string
}

// Run executes kube-test given an artifact directory, and sets settings
// required for kubetest to work with Cluster API. JUnit files are
// also gathered for inclusion in Prow.
func Run(ctx context.Context, input RunInput) error {
	if input.ClusterProxy == nil {
		return errors.New("ClusterProxy must be provided")
	}
	if input.GinkgoNodes == 0 {
		input.GinkgoNodes = DefaultGinkgoNodes
	}
	if input.GinkgoSlowSpecThreshold == 0 {
		input.GinkgoSlowSpecThreshold = 120
	}
	if input.NumberOfNodes == 0 {
		numNodes, err := countClusterNodes(ctx, input.ClusterProxy)
		if err != nil {
			return errors.Wrap(err, "Unable to count number of cluster nodes")
		}
		input.NumberOfNodes = numNodes
	}
	if input.KubernetesVersion == "" && input.ConformanceImage == "" {
		discoveredVersion, err := discoverClusterKubernetesVersion(input.ClusterProxy)
		if err != nil {
			return errors.Wrap(err, "Unable to discover server's Kubernetes version")
		}
		input.KubernetesVersion = discoveredVersion
	}
	input.ArtifactsDirectory = framework.ResolveArtifactsDirectory(input.ArtifactsDirectory)
	reportDir := path.Join(input.ArtifactsDirectory, "kubetest")
	outputDir := path.Join(reportDir, "e2e-output")
	kubetestConfigDir := path.Join(reportDir, "config")
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		return err
	}
	if err := os.MkdirAll(kubetestConfigDir, 0o750); err != nil {
		return err
	}
	ginkgoVars := map[string]string{
		"nodes":             strconv.Itoa(input.GinkgoNodes),
		"slowSpecThreshold": strconv.Itoa(input.GinkgoSlowSpecThreshold),
	}

	config, err := parseKubetestConfig(input.ConfigFilePath)
	if err != nil {
		return err
	}

	tmpKubeConfigPath, err := dockeriseKubeconfig(kubetestConfigDir, input.ClusterProxy.GetKubeconfigPath())
	if err != nil {
		return err
	}

	e2eVars := map[string]string{
		"kubeconfig":           "/tmp/kubeconfig",
		"report-dir":           "/output",
		"e2e-output-dir":       "/output/e2e-output",
		"dump-logs-on-failure": "false",
		"report-prefix":        "kubetest.",
		"num-nodes":            strconv.FormatInt(int64(input.NumberOfNodes), 10),
	}
	ginkgoArgs := buildArgs(ginkgoVars, "-")
	e2eArgs := buildArgs(e2eVars, "--")
	if input.ConformanceImage == "" {
		input.ConformanceImage = versionToConformanceImage(input.KubernetesVersion)
	}
	volumeMounts := map[string]string{
		tmpKubeConfigPath: "/tmp/kubeconfig",
		reportDir:         "/output",
	}
	for k, v := range input.ExtraMounts {
		volumeMounts[k] = v
	}
	user, err := user.Current()
	if err != nil {
		return errors.Wrap(err, "unable to determine current user")
	}
	env := map[string]string{}

	if input.KubeTestRepoListPath != "" {
		tmpKubeTestRepoListPath := path.Join(kubetestConfigDir, "repo_list.yaml")
		if err := copyFile(input.KubeTestRepoListPath, tmpKubeTestRepoListPath); err != nil {
			return err
		}
		dest := "/tmp/repo_list.yaml"
		env["KUBE_TEST_REPO_LIST"] = dest
		volumeMounts[tmpKubeTestRepoListPath] = dest
	}

	// Formulate our command arguments
	args := []string{}
	args = append(args, ginkgoArgs...)
	args = append(args, "/usr/local/bin/e2e.test")
	args = append(args, "--")
	args = append(args, e2eArgs...)
	args = append(args, config.toFlags()...)

	// Get our current working directory. Just for information, so we don't need
	// to worry about errors at this point.
	cwd, _ := os.Getwd()
	ginkgoextensions.Byf("Running e2e test: dir=%s, command=%q", cwd, args)

	containerRuntime, err := container.NewDockerClient()
	if err != nil {
		return errors.Wrap(err, "Unable to run conformance tests")
	}

	err = containerRuntime.RunContainer(ctx, &container.RunContainerInput{
		Image:           input.ConformanceImage,
		Network:         "kind",
		User:            user.Uid,
		Group:           user.Gid,
		Volumes:         volumeMounts,
		EnvironmentVars: env,
		CommandArgs:     args,
		Entrypoint:      []string{"/usr/local/bin/ginkgo"},
	}, ginkgo.GinkgoWriter)
	if err != nil {
		return errors.Wrap(err, "Unable to run conformance tests")
	}
	return framework.GatherJUnitReports(reportDir, input.ArtifactsDirectory)
}

type kubetestConfig map[string]string

func (c kubetestConfig) toFlags() []string {
	return buildArgs(c, "-")
}

func parseKubetestConfig(kubetestConfigFile string) (kubetestConfig, error) {
	conf := make(kubetestConfig)
	data, err := os.ReadFile(kubetestConfigFile)
	if err != nil {
		return nil, fmt.Errorf("unable to read kubetest config file %s: %w", kubetestConfigFile, err)
	}
	if err := yaml.Unmarshal(data, &conf); err != nil {
		return nil, fmt.Errorf("unable to parse kubetest config file %s as valid, non-nested YAML: %w", kubetestConfigFile, err)
	}
	// Apply defaults
	if _, ok := conf["provider"]; !ok {
		conf["provider"] = "skeleton"
	}
	return conf, nil
}

func isUsingCIArtifactsVersion(k8sVersion string) bool {
	return strings.Contains(k8sVersion, "-")
}

func discoverClusterKubernetesVersion(proxy framework.ClusterProxy) (string, error) {
	config := proxy.GetRESTConfig()
	discoverClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return "", err
	}
	serverVersionInfo, err := discoverClient.ServerVersion()
	if err != nil {
		return "", err
	}

	return serverVersionInfo.String(), nil
}

func dockeriseKubeconfig(kubetestConfigDir string, kubeConfigPath string) (string, error) {
	kubeConfig, err := clientcmd.LoadFromFile(kubeConfigPath)
	if err != nil {
		return "", err
	}
	newPath := path.Join(kubetestConfigDir, "kubeconfig")

	// On CAPD, if not running on Linux, we need to use Docker's proxy to connect back to the host
	// to the CAPD cluster. Moby on Linux doesn't use the host.docker.internal DNS name.
	if runtime.GOOS != "linux" {
		for i := range kubeConfig.Clusters {
			kubeConfig.Clusters[i].Server = strings.ReplaceAll(kubeConfig.Clusters[i].Server, "127.0.0.1", "host.docker.internal")
		}
	}
	if err := clientcmd.WriteToFile(*kubeConfig, newPath); err != nil {
		return "", err
	}
	return newPath, nil
}

func countClusterNodes(ctx context.Context, proxy framework.ClusterProxy) (int, error) {
	nodeList, err := proxy.GetClientSet().CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, errors.Wrap(err, "Unable to count nodes")
	}
	return len(nodeList.Items), nil
}

func versionToConformanceImage(kubernetesVersion string) string {
	k8sVersion := strings.ReplaceAll(kubernetesVersion, "+", "_")
	if isUsingCIArtifactsVersion(kubernetesVersion) {
		return ciArtifactImage + ":" + k8sVersion
	}
	return standardImage + ":" + k8sVersion
}

// buildArgs converts a string map to the format --key=value.
func buildArgs(kv map[string]string, flagMarker string) []string {
	args := make([]string, len(kv))
	i := 0
	for k, v := range kv {
		args[i] = flagMarker + k + "=" + v
		i++
	}
	return args
}
