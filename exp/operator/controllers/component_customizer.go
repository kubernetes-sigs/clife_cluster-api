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

package controllers

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/utils/pointer"

	operatorv1 "sigs.k8s.io/cluster-api/exp/operator/api/v1alpha1"
	"sigs.k8s.io/cluster-api/exp/operator/controllers/genericprovider"
	"sigs.k8s.io/cluster-api/util"
)

const (
	deploymentKind = "Deployment"
)

func imageMetaToURL(im *operatorv1.ImageMeta) string {
	tag := "latest"
	if im.Tag != nil {
		tag = *im.Tag
	}
	return strings.Join([]string{*im.Repository, *im.Name}, "/") + ":" + tag
}

func customizeContainer(cSpec operatorv1.ContainerSpec, d *appsv1.Deployment) {
	for j, c := range d.Spec.Template.Spec.Containers {
		if c.Name == cSpec.Name {
			for an, av := range cSpec.Args {
				c.Args = removeArg(c.Args, an)
				c.Args = append(c.Args, fmt.Sprintf("%s=%s", an, av))
			}
			for _, se := range cSpec.Env {
				c.Env = removeEnv(c.Env, se.Name)
				c.Env = append(c.Env, se)
			}
			if cSpec.Resources != nil {
				c.Resources = *cSpec.Resources
			}
			if cSpec.Image != nil && cSpec.Image.Name != nil && cSpec.Image.Repository != nil {
				c.Image = imageMetaToURL(cSpec.Image)
			}
		}
		d.Spec.Template.Spec.Containers[j] = c
	}
}

func removeEnv(envs []corev1.EnvVar, name string) []corev1.EnvVar {
	for i, a := range envs {
		if a.Name == name {
			copy(envs[i:], envs[i+1:])
			return envs[:len(envs)-1]
		}
	}

	return envs
}

func removeArg(args []string, name string) []string {
	for i, a := range args {
		if strings.HasPrefix(a, name+"=") {
			copy(args[i:], args[i+1:])
			return args[:len(args)-1]
		}
	}

	return args
}

func customizeDeployment(dSpec *operatorv1.DeploymentSpec, d *appsv1.Deployment) {
	if dSpec.Replicas != nil {
		d.Spec.Replicas = pointer.Int32Ptr(int32(*dSpec.Replicas))
	}
	if dSpec.Affinity != nil {
		d.Spec.Template.Spec.Affinity = dSpec.Affinity
	}
	if dSpec.NodeSelector != nil {
		d.Spec.Template.Spec.NodeSelector = dSpec.NodeSelector
	}
	if dSpec.Tolerations != nil {
		d.Spec.Template.Spec.Tolerations = dSpec.Tolerations
	}

	for _, pc := range dSpec.Containers {
		customizeContainer(pc, d)
	}
}

func customizeSharedObjectsFn(provider genericprovider.GenericProvider) func(objs []unstructured.Unstructured) ([]unstructured.Unstructured, error) {
	return func(objs []unstructured.Unstructured) ([]unstructured.Unstructured, error) {
		for i := range objs {
			o := objs[i]

			// Set the owner references so that when the provider is deleted, the components are also deleted.
			o.SetOwnerReferences(util.EnsureOwnerRef(provider.GetOwnerReferences(), metav1.OwnerReference{
				APIVersion: operatorv1.GroupVersion.String(),
				Kind:       provider.GetObjectKind().GroupVersionKind().Kind,
				Name:       provider.GetName(),
				UID:        provider.GetUID(),
			}))
			objs[i] = o
		}
		return objs, nil
	}
}

func customizeInstanceObjectsFn(provider genericprovider.GenericProvider) func(objs []unstructured.Unstructured) ([]unstructured.Unstructured, error) {
	return func(objs []unstructured.Unstructured) ([]unstructured.Unstructured, error) {
		for i := range objs {
			o := objs[i]

			// Set the owner references so that when the provider is deleted, the components are also deleted.
			o.SetOwnerReferences(util.EnsureOwnerRef(provider.GetOwnerReferences(), metav1.OwnerReference{
				APIVersion: operatorv1.GroupVersion.String(),
				Kind:       provider.GetObjectKind().GroupVersionKind().Kind,
				Name:       provider.GetName(),
				UID:        provider.GetUID(),
			}))

			if provider.GetSpec().Deployment != nil && o.GetKind() == deploymentKind {
				// Convert Unstructured into a typed object
				d := &appsv1.Deployment{}
				if err := scheme.Scheme.Convert(&o, d, nil); err != nil {
					return nil, err
				}
				customizeDeployment(provider.GetSpec().Deployment, d)

				// Convert Deployment back to Unstructured
				if err := scheme.Scheme.Convert(d, &o, nil); err != nil {
					return nil, err
				}
			}
			objs[i] = o
		}
		return objs, nil
	}
}
