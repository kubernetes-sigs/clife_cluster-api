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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// getNestedRef returns the ref value of a nested field.
// NOTE: This function assumes the reference either exists in the CRD, and it that case the value is required,
// or it does not exists in the CRD.
func getNestedRef(obj *unstructured.Unstructured, fields ...string) *corev1.ObjectReference {
	if obj == nil {
		return nil
	}
	ref, ok, err := unstructured.NestedMap(obj.UnstructuredContent(), fields...)
	if !ok || err != nil {
		return nil
	}
	return &corev1.ObjectReference{
		Kind:       ref["kind"].(string),
		Namespace:  ref["namespace"].(string),
		Name:       ref["name"].(string),
		APIVersion: ref["apiVersion"].(string),
	}
}

// setNestedRef sets the value of a nested field to a reference to the refObj provided.
func setNestedRef(obj, refObj *unstructured.Unstructured, fields ...string) error {
	ref := map[string]interface{}{
		"kind":       refObj.GetKind(),
		"namespace":  refObj.GetNamespace(),
		"name":       refObj.GetName(),
		"apiVersion": refObj.GetAPIVersion(),
	}
	return unstructured.SetNestedField(obj.UnstructuredContent(), ref, fields...)
}

func objToRef(obj client.Object) *corev1.ObjectReference {
	gvk := obj.GetObjectKind().GroupVersionKind()
	return &corev1.ObjectReference{
		Kind:       gvk.Kind,
		APIVersion: gvk.GroupVersion().String(),
		Namespace:  obj.GetNamespace(),
		Name:       obj.GetName(),
	}
}
