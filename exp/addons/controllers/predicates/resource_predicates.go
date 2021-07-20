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

package predicates

import (
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	addonsv1 "sigs.k8s.io/cluster-api/exp/addons/api/v1alpha3"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// ResourceCreate returns a predicate that returns true for a create event
func ResourceCreate(logger logr.Logger) predicate.Funcs {
	return predicate.Funcs{
		CreateFunc:  func(e event.CreateEvent) bool { return true },
		UpdateFunc:  func(e event.UpdateEvent) bool { return false },
		DeleteFunc:  func(e event.DeleteEvent) bool { return false },
		GenericFunc: func(e event.GenericEvent) bool { return false },
	}
}

// AddonsSecretCreate returns a predicate that returns true for a Secret create event if in addons Secret type
// DEPRECATED: use ResourceCreate() predicate instead because Secret type is not reachable when Secrets are cached as PartialObjectMetadata.
func AddonsSecretCreate(logger logr.Logger) predicate.Funcs {
	log := logger.WithValues("predicate", "SecretCreateOrUpdate")

	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			log = log.WithValues("eventType", "create")
			s, ok := e.Object.(*corev1.Secret)
			if !ok {
				log.V(4).Info("Expected Secret", "secret", e.Object.GetObjectKind().GroupVersionKind().String())
				return false
			}
			if string(s.Type) != string(addonsv1.ClusterResourceSetSecretType) {
				log.V(4).Info("Expected Secret Type", "type", addonsv1.SecretClusterResourceSetResourceKind,
					"got", string(s.Type))
				return false
			}
			return true
		},
		UpdateFunc:  func(e event.UpdateEvent) bool { return false },
		DeleteFunc:  func(e event.DeleteEvent) bool { return false },
		GenericFunc: func(e event.GenericEvent) bool { return false },
	}
}
