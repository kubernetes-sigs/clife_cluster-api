package predicates

import (
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// NodeConditionUpdated returns a predicate that returns true for node condition updated.
func NodeConditionUpdated(logger logr.Logger) predicate.Funcs {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			log := logger.WithValues("predicate", "NodeConditionUpdated", "eventType", "update")

			oldNode, ok := e.ObjectOld.(*corev1.Node)
			if !ok {
				log.V(4).Info("Expected Node", "type", e.ObjectOld.GetObjectKind().GroupVersionKind().String())
				return false
			}
			newNode, ok := e.ObjectNew.(*corev1.Node)
			if !ok {
				log.V(4).Info("Expected Node", "type", e.ObjectOld.GetObjectKind().GroupVersionKind().String())
				return false
			}
			oldConditionMap := make(map[corev1.NodeConditionType]corev1.NodeCondition)
			for _, c := range oldNode.Status.Conditions {
				oldConditionMap[c.Type] = c
			}
			for _, nc := range newNode.Status.Conditions {
				if nc.Status != oldConditionMap[nc.Type].Status {
					return true
				}
			}
			return false
		},
		CreateFunc:  func(e event.CreateEvent) bool { return false },
		DeleteFunc:  func(e event.DeleteEvent) bool { return false },
		GenericFunc: func(e event.GenericEvent) bool { return false },
	}
}
