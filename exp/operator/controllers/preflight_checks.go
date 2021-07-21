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
	"context"
	"fmt"

	"github.com/pkg/errors"
	"sigs.k8s.io/cluster-api/api/v1alpha4"
	operatorv1 "sigs.k8s.io/cluster-api/exp/operator/api/v1alpha1"
	"sigs.k8s.io/cluster-api/exp/operator/controllers/genericprovider"
	"sigs.k8s.io/cluster-api/exp/operator/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	coreProvider = "CoreProvider"
)

var (
	moreThanOneCoreProviderInstanceExistsMessage = "CoreProvider already exists in the cluster. Only one is allowed."
	moreThanOneProviderInstanceExistsMessage     = "There is already a %s with name %s in the cluster. Only one is allowed."
)

func preflightChecks(ctx context.Context, c client.Client, provider genericprovider.GenericProvider, providerList genericprovider.GenericProviderList) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	log.V(4).Info("Performing preflight checks.")

	if err := c.List(ctx, providerList.GetObject()); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "failed to list providers")
	}

	for _, p := range providerList.GetItems() {
		// Skip if provider in the list is the same as provider it's compared with.
		if p.GetNamespace() == provider.GetNamespace() && p.GetName() == provider.GetName() {
			continue
		}

		preflightFalseCondition := conditions.FalseCondition(
			operatorv1.PreflightCheckCondition,
			operatorv1.MoreThanOneProviderInstanceExistsReason,
			v1alpha4.ConditionSeverityWarning,
			"",
		)

		// CoreProvider is a singleton resource, more than one instances should not exist
		if util.IsCoreProvider(p) {
			log.V(4).Info(moreThanOneCoreProviderInstanceExistsMessage)
			preflightFalseCondition.Message = moreThanOneCoreProviderInstanceExistsMessage
			conditions.Set(provider, preflightFalseCondition)
			return ctrl.Result{RequeueAfter: preflightFailedRequeueAfter}, nil
		}

		// For any other provider we should check that instances with similar name exist in any namespace
		if p.GetObjectKind().GroupVersionKind().Kind != coreProvider && p.GetName() == provider.GetName() {
			log.V(4).Info(moreThanOneProviderInstanceExistsMessage, p.GetName(), p.GetNamespace())
			preflightFalseCondition.Message = fmt.Sprintf(moreThanOneProviderInstanceExistsMessage, p.GetName(), p.GetNamespace())
			conditions.Set(provider, preflightFalseCondition)
			return ctrl.Result{RequeueAfter: preflightFailedRequeueAfter}, nil
		}
	}

	conditions.Set(provider, conditions.TrueCondition(operatorv1.PreflightCheckCondition))
	return ctrl.Result{}, nil
}
