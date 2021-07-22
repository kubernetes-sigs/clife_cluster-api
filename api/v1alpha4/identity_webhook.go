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

package v1alpha4

import (
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// ValidateCreate validates the creation of InfraClusterScopedIdentityAllowedNamespaces
func (a *InfraClusterScopedIdentityAllowedNamespaces) ValidateCreate() error {
	return a.validate(nil)
}

// ValidateUpdate validates the update of InfraClusterScopedIdentityAllowedNamespaces
func (a *InfraClusterScopedIdentityAllowedNamespaces) ValidateUpdate(oldA *InfraClusterScopedIdentityAllowedNamespaces) error {
	return a.validate(oldA)
}

func (a *InfraClusterScopedIdentityAllowedNamespaces) validate(oldA *InfraClusterScopedIdentityAllowedNamespaces) error {
	if len(a.List) == 0 {
		return nil
	}
	if len(a.Selector.MatchExpressions) != 0 || len(a.Selector.MatchLabels) != 0 {
		return field.Invalid(field.NewPath("spec", "allowedNamespaces", "selector"), a.Selector, "selector cannot be set simultaneously with spec.allowedNamespaces.list")
	}
	return nil
}

// ValidateDelete validates the delete of AllowedNamespaces
func (a *InfraClusterScopedIdentityAllowedNamespaces) ValidateDelete() error {
	return nil
}

// ValidateCreate validates the creation of AllowedNamespaces
func (i *InfraClusterScopedIdentityCommonSpec) ValidateCreate() error {
	return i.AllowedNamespaces.ValidateCreate()
}

// ValidateUpdate validates the update of AllowedNamespaces
func (i *InfraClusterScopedIdentityCommonSpec) ValidateUpdate(oldA *InfraClusterScopedIdentityCommonSpec) error {
	return i.AllowedNamespaces.ValidateUpdate(&i.AllowedNamespaces)
}

// ValidateDelete validates the delete of AllowedNamespaces
func (i *InfraClusterScopedIdentityCommonSpec) ValidateDelete() error {
	return nil
}
