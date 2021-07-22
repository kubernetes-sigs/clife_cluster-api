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
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/gobuffalo/flect"
	"github.com/gosuri/uitable"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/duration"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha4"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/client"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/client/tree"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	firstElemPrefix = `├─`
	lastElemPrefix  = `└─`
	indent          = "  "
	pipe            = `│ `
)

var (
	gray   = color.New(color.FgHiBlack)
	red    = color.New(color.FgRed)
	green  = color.New(color.FgGreen)
	yellow = color.New(color.FgYellow)
	white  = color.New(color.FgWhite)
	cyan   = color.New(color.FgCyan)
)

type describeClusterOptions struct {
	kubeconfig        string
	kubeconfigContext string

	namespace           string
	showOtherConditions string
	disableNoEcho       bool
	disableGrouping     bool
}

var dc = &describeClusterOptions{}

var describeClusterClusterCmd = &cobra.Command{
	Use:   "cluster",
	Short: "Describe workload clusters.",
	Long: LongDesc(`
		Provide an "at glance" view of a Cluster API cluster designed to help the user in quickly
		understanding if there are problems and where.
		.`),

	Example: Examples(`
		# Describe the cluster named test-1.
		clusterctl describe cluster test-1

		# Describe the cluster named test-1 showing all the conditions for the KubeadmControlPlane object kind.
		clusterctl describe cluster test-1 --show-conditions KubeadmControlPlane

		# Describe the cluster named test-1 showing all the conditions for a specific machine.
		clusterctl describe cluster test-1 --show-conditions Machine/m1

		# Describe the cluster named test-1 disabling automatic grouping of objects with the same ready condition
		# e.g. un-group all the machines with Ready=true instead of showing a single group node.
		clusterctl describe cluster test-1 --disable-grouping

		# Describe the cluster named test-1 disabling automatic echo suppression
        # e.g. show the infrastructure machine objects, no matter if the current state is already reported by the machine's Ready condition.
		clusterctl describe cluster test-1`),

	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDescribeCluster(args[0])
	},
}

func init() {
	describeClusterClusterCmd.Flags().StringVar(&dc.kubeconfig, "kubeconfig", "",
		"Path to a kubeconfig file to use for the management cluster. If empty, default discovery rules apply.")
	describeClusterClusterCmd.Flags().StringVar(&dc.kubeconfigContext, "kubeconfig-context", "",
		"Context to be used within the kubeconfig file. If empty, current context will be used.")
	describeClusterClusterCmd.Flags().StringVarP(&dc.namespace, "namespace", "n", "",
		"The namespace where the workload cluster is located. If unspecified, the current namespace will be used.")

	describeClusterClusterCmd.Flags().StringVar(&dc.showOtherConditions, "show-conditions", "",
		" list of comma separated kind or kind/name for which the command should show all the object's conditions (use 'all' to show conditions for everything).")
	describeClusterClusterCmd.Flags().BoolVar(&dc.disableNoEcho, "disable-no-echo", false, ""+
		"Disable hiding of a MachineInfrastructure and BootstrapConfig when ready condition is true or it has the Status, Severity and Reason of the machine's object.")
	describeClusterClusterCmd.Flags().BoolVar(&dc.disableGrouping, "disable-grouping", false,
		"Disable grouping machines when ready condition has the same Status, Severity and Reason.")

	describeCmd.AddCommand(describeClusterClusterCmd)
}

func runDescribeCluster(name string) error {
	c, err := client.New(cfgFile)
	if err != nil {
		return err
	}

	tree, err := c.DescribeCluster(client.DescribeClusterOptions{
		Kubeconfig:          client.Kubeconfig{Path: dc.kubeconfig, Context: dc.kubeconfigContext},
		Namespace:           dc.namespace,
		ClusterName:         name,
		ShowOtherConditions: dc.showOtherConditions,
		DisableNoEcho:       dc.disableNoEcho,
		DisableGrouping:     dc.disableGrouping,
	})
	if err != nil {
		return err
	}

	printObjectTree(tree)
	return nil
}

// printObjectTree prints the cluster status to stdout.
func printObjectTree(tree *tree.ObjectTree) {
	// Creates the output table
	tbl := uitable.New()
	tbl.Separator = "  "
	tbl.AddRow("NAME", "READY", "SEVERITY", "REASON", "SINCE", "MESSAGE")

	// Add row for the root object, the cluster, and recursively for all the nodes representing the cluster status.
	addObjectRow("", tbl, tree, tree.GetRoot())

	// Prints the output table
	fmt.Fprintln(color.Error, tbl)
}

// addObjectRow add a row for a given object, and recursively for all the object's children.
// NOTE: each row name gets a prefix, that generates a tree view like representation.
func addObjectRow(prefix string, tbl *uitable.Table, objectTree *tree.ObjectTree, obj ctrlclient.Object) {
	// Gets the descriptor for the object's ready condition, if any.
	readyDescriptor := conditionDescriptor{readyColor: gray}
	if ready := tree.GetReadyCondition(obj); ready != nil {
		readyDescriptor = newConditionDescriptor(ready)
	}

	// If the object is a group object, override the condition message with the list of objects in the group. e.g machine-1, machine-2, ...
	if tree.IsGroupObject(obj) {
		items := strings.Split(tree.GetGroupItems(obj), tree.GroupItemsSeparator)
		if len(items) <= 2 {
			readyDescriptor.message = gray.Sprintf("See %s", strings.Join(items, tree.GroupItemsSeparator))
		} else {
			readyDescriptor.message = gray.Sprintf("See %s, ...", strings.Join(items[:2], tree.GroupItemsSeparator))
		}
	}

	// Gets the row name for the object.
	// NOTE: The object name gets manipulated in order to improve readability.
	name := getRowName(obj)

	// Add the row representing the object that includes
	// - The row name with the tree view prefix.
	// - The object's ready condition.
	tbl.AddRow(
		fmt.Sprintf("%s%s", gray.Sprint(prefix), name),
		readyDescriptor.readyColor.Sprint(readyDescriptor.status),
		readyDescriptor.readyColor.Sprint(readyDescriptor.severity),
		readyDescriptor.readyColor.Sprint(readyDescriptor.reason),
		readyDescriptor.age,
		readyDescriptor.message)

	// If it is required to show all the conditions for the object, add a row for each object's conditions.
	if tree.IsShowConditionsObject(obj) {
		addOtherConditions(prefix, tbl, objectTree, obj)
	}

	// Add a row for each object's children, taking care of updating the tree view prefix.
	// NOTE: Children objects are sorted by row name for better readability.
	childrenObj := objectTree.GetObjectsByParent(obj.GetUID())
	sort.Slice(childrenObj, func(i, j int) bool {
		return getRowName(childrenObj[i]) < getRowName(childrenObj[j])
	})

	for i, child := range childrenObj {
		addObjectRow(getChildPrefix(prefix, i, len(childrenObj)), tbl, objectTree, child)
	}
}

// addOtherConditions adds a row for each object condition except the ready condition,
// which is already represented on the object's main row.
func addOtherConditions(prefix string, tbl *uitable.Table, objectTree *tree.ObjectTree, obj ctrlclient.Object) {
	// Add a row for each other condition, taking care of updating the tree view prefix.
	// In this case the tree prefix get a filler, to indent conditions from objects, and eventually a
	// and additional pipe if the object has children that should be presented after the conditions.
	filler := strings.Repeat(" ", 10)
	childrenPipe := indent
	if objectTree.IsObjectWithChild(obj.GetUID()) {
		childrenPipe = pipe
	}

	otherConditions := tree.GetOtherConditions(obj)
	for i := range otherConditions {
		otherCondition := otherConditions[i]
		otherDescriptor := newConditionDescriptor(otherCondition)
		otherConditionPrefix := getChildPrefix(prefix+childrenPipe+filler, i, len(otherConditions))
		tbl.AddRow(
			fmt.Sprintf("%s%s", gray.Sprint(otherConditionPrefix), cyan.Sprint(otherCondition.Type)),
			otherDescriptor.readyColor.Sprint(otherDescriptor.status),
			otherDescriptor.readyColor.Sprint(otherDescriptor.severity),
			otherDescriptor.readyColor.Sprint(otherDescriptor.reason),
			otherDescriptor.age,
			otherDescriptor.message)
	}
}

// getChildPrefix return the tree view prefix for a row representing a child object.
func getChildPrefix(currentPrefix string, childIndex, childCount int) string {
	nextPrefix := currentPrefix

	// Alter the current prefix for hosting the next child object:

	// All ├─ should be replaced by |, so all the existing hierarchic dependencies are carried on
	nextPrefix = strings.ReplaceAll(nextPrefix, firstElemPrefix, pipe)
	// All └─ should be replaced by " " because we are under the last element of the tree (nothing to carry on)
	nextPrefix = strings.ReplaceAll(nextPrefix, lastElemPrefix, strings.Repeat(" ", len([]rune(lastElemPrefix))))

	// Add the prefix for the new child object (├─ for the firsts children, └─ for the last children).
	if childIndex < childCount-1 {
		return nextPrefix + firstElemPrefix
	}
	return nextPrefix + lastElemPrefix
}

// getRowName returns the object name in the tree view according to following rules:
// - group objects are represented as #of objects kind, e.g. 3 Machines...
// - other virtual objects are represented using the object name, e.g. Workers
// - objects with a meta name are represented as meta name - (kind/name), e.g. ClusterInfrastructure - DockerCluster/test1
// - other objects are represented as kind/name, e.g.Machine/test1-md-0-779b87ff56-642vs
// - if the object is being deleted, a prefix will be added.
func getRowName(obj ctrlclient.Object) string {
	if tree.IsGroupObject(obj) {
		items := strings.Split(tree.GetGroupItems(obj), tree.GroupItemsSeparator)
		kind := flect.Pluralize(strings.TrimSuffix(obj.GetObjectKind().GroupVersionKind().Kind, "Group"))
		return white.Add(color.Bold).Sprintf("%d %s...", len(items), kind)
	}

	if tree.IsVirtualObject(obj) {
		return obj.GetName()
	}

	objName := fmt.Sprintf("%s/%s",
		obj.GetObjectKind().GroupVersionKind().Kind,
		color.New(color.Bold).Sprint(obj.GetName()))

	name := objName
	if objectPrefix := tree.GetMetaName(obj); objectPrefix != "" {
		name = fmt.Sprintf("%s - %s", objectPrefix, gray.Sprintf(name))
	}

	if !obj.GetDeletionTimestamp().IsZero() {
		name = fmt.Sprintf("%s %s", red.Sprintf("!! DELETED !!"), name)
	}

	return name
}

// conditionDescriptor contains all the info for representing a condition.
type conditionDescriptor struct {
	readyColor *color.Color
	age        string
	status     string
	severity   string
	reason     string
	message    string
}

// newConditionDescriptor returns a conditionDescriptor for the given condition.
func newConditionDescriptor(c *clusterv1.Condition) conditionDescriptor {
	v := conditionDescriptor{}

	v.status = string(c.Status)
	v.severity = string(c.Severity)
	v.reason = c.Reason
	v.message = c.Message

	// Eventually cut the message to keep the table dimension under control.
	if len(v.message) > 100 {
		v.message = fmt.Sprintf("%s ...", v.message[:100])
	}

	// Compute the condition age.
	v.age = duration.HumanDuration(time.Since(c.LastTransitionTime.Time))

	// Determine the color to be used for showing the conditions according to Status and Severity in case Status is false.
	switch c.Status {
	case corev1.ConditionTrue:
		v.readyColor = green
	case corev1.ConditionFalse, corev1.ConditionUnknown:
		switch c.Severity {
		case clusterv1.ConditionSeverityError:
			v.readyColor = red
		case clusterv1.ConditionSeverityWarning:
			v.readyColor = yellow
		default:
			v.readyColor = white
		}
	default:
		v.readyColor = gray
	}

	return v
}
