/*
Copyright 2017 The Kubernetes Authors.

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

package util

import (
	"context"
	"fmt"
	"io"
	"k8s.io/apimachinery/pkg/util/json"
	"math/rand"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/klog"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// CharSet defines the alphanumeric set for random string generation
	CharSet = "0123456789abcdefghijklmnopqrstuvwxyz"
)

var (
	r = rand.New(rand.NewSource(time.Now().UnixNano()))
)

// RandomToken returns a random token
func RandomToken() string {
	return fmt.Sprintf("%s.%s", RandomString(6), RandomString(16))
}

// RandomString returns a random alphanumeric string
func RandomString(n int) string {
	result := make([]byte, n)
	for i := range result {
		result[i] = CharSet[r.Intn(len(CharSet))]
	}
	return string(result)
}

// GetControlPlaneMachine returns the control plane machine from a slice
func GetControlPlaneMachine(machines []*clusterv1.Machine) *clusterv1.Machine {
	for _, machine := range machines {
		if IsControlPlaneMachine(machine) {
			return machine
		}
	}
	return nil
}

// MachineP converts a slice of machines into a slice of machine pointers
func MachineP(machines []clusterv1.Machine) []*clusterv1.Machine {
	// Convert to list of pointers
	var ret []*clusterv1.Machine
	for _, machine := range machines {
		ret = append(ret, machine.DeepCopy())
	}
	return ret
}

// Home returns the user home directory
func Home() string {
	home := os.Getenv("HOME")
	if strings.Contains(home, "root") {
		return "/root"
	}

	usr, err := user.Current()
	if err != nil {
		klog.Warningf("unable to find user: %v", err)
		return ""
	}
	return usr.HomeDir
}

// GetDefaultKubeConfigPath returns the standard user kubeconfig
func GetDefaultKubeConfigPath() string {
	localDir := fmt.Sprintf("%s/.kube", Home())
	if _, err := os.Stat(localDir); os.IsNotExist(err) {
		if err := os.Mkdir(localDir, 0777); err != nil {
			klog.Fatal(err)
		}
	}
	return fmt.Sprintf("%s/config", localDir)
}

// GetMachineIfExists gets a machine from the API server if it exists
func GetMachineIfExists(c client.Client, namespace, name string) (*clusterv1.Machine, error) {
	if c == nil {
		// Being called before k8s is setup as part of control plane VM creation
		return nil, nil
	}

	// Machines are identified by name
	machine := &clusterv1.Machine{}
	err := c.Get(context.Background(), client.ObjectKey{Namespace: namespace, Name: name}, machine)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	return machine, nil
}

// IsControlPlaneMachine checks machine is a control plane node
// TODO(robertbailey): Remove this function
func IsControlPlaneMachine(machine *clusterv1.Machine) bool {
	return machine.Spec.Versions.ControlPlane != ""
}

// IsNodeReady returns true if a node is ready
func IsNodeReady(node *v1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == v1.NodeReady {
			return condition.Status == v1.ConditionTrue
		}
	}

	return false
}

// Copy deep copies a Machine object
func Copy(m *clusterv1.Machine) *clusterv1.Machine {
	ret := &clusterv1.Machine{}
	ret.APIVersion = m.APIVersion
	ret.Kind = m.Kind
	ret.ClusterName = m.ClusterName
	ret.GenerateName = m.GenerateName
	ret.Name = m.Name
	ret.Namespace = m.Namespace
	m.Spec.DeepCopyInto(&ret.Spec)
	return ret
}

// ExecCommand Executes a local command in the current shell
func ExecCommand(name string, args ...string) string {
	cmdOut, err := exec.Command(name, args...).Output()
	if err != nil {
		s := strings.Join(append([]string{name}, args...), " ")
		klog.Errorf("error executing command %q: %v", s, err)
	}
	return string(cmdOut)
}

// Filter filters a list for a string
func Filter(list []string, strToFilter string) (newList []string) {
	for _, item := range list {
		if item != strToFilter {
			newList = append(newList, item)
		}
	}
	return
}

// Contains returns true if a list contains a string
func Contains(list []string, strToSearch string) bool {
	for _, item := range list {
		if item == strToSearch {
			return true
		}
	}
	return false
}

// GetNamespaceOrDefault returns the default namespace if given empty
// output
func GetNamespaceOrDefault(namespace string) string {
	if namespace == "" {
		return v1.NamespaceDefault
	}
	return namespace
}

// ParseClusterYaml parses a YAML file for cluster objects
func ParseClusterYaml(file string) (*clusterv1.Cluster, error) {
	reader, err := os.Open(file)

	if err != nil {
		return nil, err
	}

	defer reader.Close()

	decoder := yaml.NewYAMLOrJSONDecoder(reader, 32)

	bytes, err := decodeClusterV1Kinds(decoder, "Cluster")
	if err != nil {
		return nil, err
	}

	var cluster clusterv1.Cluster

	if err := json.Unmarshal(bytes[0], &cluster); err != nil {
		return nil, err
	}

	return &cluster, nil
}

// ParseMachinesYaml extracts machine objects from a file
func ParseMachinesYaml(file string) ([]*clusterv1.Machine, error) {
	reader, err := os.Open(file)

	if err != nil {
		return nil, err
	}

	defer reader.Close()

	decoder := yaml.NewYAMLOrJSONDecoder(reader, 32)
	machineList, err := decodeMachineLists(decoder)

	if err != nil {
		return nil, err
	}

	// Will reread the file to find items which aren't a list.
	// TODO: Make the Kind field mandatory on machines.yaml and then use the
	// universal decoder instead of doing this.
	reader.Seek(0, 0)
	bytes, err := decodeClusterV1Kinds(decoder, "Machine")

	// Original set of MachineLists did not have Kind field
	if err != nil && !isMissingKind(err) {
		return nil, err
	}

	machines := []clusterv1.Machine{}

	for _, m := range bytes {
		var machine clusterv1.Machine
		err = json.Unmarshal(m, &machine)
		if err != nil {
			return nil, err
		}
		machines = append(machines, machine)
	}

	machinesP := MachineP(machines)

	return append(machinesP, machineList...), nil
}

// decodeMachineLists extracts MachineLists from a byte reader
func decodeMachineLists(decoder *yaml.YAMLOrJSONDecoder) ([]*clusterv1.Machine, error) {

	outs := []clusterv1.Machine{}

	for {
		var out clusterv1.MachineList
		err := decoder.Decode(&out)

		if err == io.EOF {
			break
		}
		outs = append(outs, out.Items...)
	}
	return MachineP(outs), nil
}

// isMissingKind reimplements runtime.IsMissingKind as the YAMLOrJSONDecoder
// hides the error type
func isMissingKind(err error) bool {
	return strings.Contains(err.Error(), "Object 'Kind' is missing in")
}

// decodeClusterV1Kinds returns a slice of objects matching the clusterv1 kind
func decodeClusterV1Kinds(decoder *yaml.YAMLOrJSONDecoder, kind string) ([][]byte, error) {

	outs := [][]byte{}

	for {
		var out unstructured.Unstructured
		err := decoder.Decode(&out)

		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		if out.GetKind() == kind && out.GetAPIVersion() == clusterv1.SchemeGroupVersion.String() {
			var marshaled []byte
			marshaled, err = out.MarshalJSON()
			if err != nil {
				return outs, err
			}
			outs = append(outs, marshaled)
		}
	}

	return outs, nil
}
