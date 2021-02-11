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

package cluster

import (
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/version"
	clusterctlv1 "sigs.k8s.io/cluster-api/cmd/clusterctl/api/v1alpha4"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/client/config"
	"sigs.k8s.io/cluster-api/cmd/clusterctl/client/repository"
	logf "sigs.k8s.io/cluster-api/cmd/clusterctl/log"
)

// ProviderUpgrader defines methods for supporting provider upgrade.
type ProviderUpgrader interface {
	// Plan returns a set of suggested Upgrade plans for the cluster, and more specifically:
	// - Each management group gets separated upgrade plans.
	// - For each management group, an upgrade plan will be generated for each API Version of Cluster API (contract) available, e.g.
	//   - Upgrade to the latest version in the the v1alpha2 series: ....
	//   - Upgrade to the latest version in the the v1alpha3 series: ....
	Plan() ([]UpgradePlan, error)

	// ApplyPlan executes an upgrade following an UpgradePlan generated by clusterctl.
	ApplyPlan(coreProvider clusterctlv1.Provider, clusterAPIVersion string) error

	// ApplyCustomPlan plan executes an upgrade using the UpgradeItems provided by the user.
	ApplyCustomPlan(coreProvider clusterctlv1.Provider, providersToUpgrade ...UpgradeItem) error
}

// UpgradePlan defines a list of possible upgrade targets for a management group.
type UpgradePlan struct {
	Contract     string
	CoreProvider clusterctlv1.Provider
	Providers    []UpgradeItem
}

// UpgradeRef returns a string identifying the upgrade plan; this string is derived by the core provider which is
// unique for each management group.
func (u *UpgradePlan) UpgradeRef() string {
	return u.CoreProvider.InstanceName()
}

// isPartialUpgrade returns true if at least one upgradeItem in the plan does not have a target version.
func (u *UpgradePlan) isPartialUpgrade() bool {
	for _, i := range u.Providers {
		if i.NextVersion == "" {
			return true
		}
	}
	return false
}

// UpgradeItem defines a possible upgrade target for a provider in the management group.
type UpgradeItem struct {
	clusterctlv1.Provider
	NextVersion string
}

// UpgradeRef returns a string identifying the upgrade item; this string is derived by the provider.
func (u *UpgradeItem) UpgradeRef() string {
	return u.InstanceName()
}

type providerUpgrader struct {
	configClient            config.Client
	repositoryClientFactory RepositoryClientFactory
	providerInventory       InventoryClient
	providerComponents      ComponentsClient
}

var _ ProviderUpgrader = &providerUpgrader{}

func (u *providerUpgrader) Plan() ([]UpgradePlan, error) {
	log := logf.Log
	log.Info("Checking new release availability...")

	managementGroups, err := u.providerInventory.GetManagementGroups()
	if err != nil {
		return nil, err
	}

	var ret []UpgradePlan
	for _, managementGroup := range managementGroups {
		// The core provider is driving all the plan logic for each management group, because all the providers
		// in a management group are expected to support the same API Version of Cluster API (contract).
		// e.g if the core provider supports v1alpha3, all the providers in the same management group should support v1alpha3 as well;
		// all the providers in the management group can upgrade to the latest release supporting v1alpha3, or if available,
		// or if available, all the providers in the management group can upgrade to the latest release supporting v1alpha4.

		// Gets the upgrade info for the core provider.
		coreUpgradeInfo, err := u.getUpgradeInfo(managementGroup.CoreProvider)
		if err != nil {
			return nil, err
		}

		// Identifies the API Version of Cluster API (contract) that we should consider for the management group update (Nb. the core provider is driving the entire management group).
		// This includes the current contract (e.g. v1alpha3) and the new one available, if any.
		contractsForUpgrade := coreUpgradeInfo.getContractsForUpgrade()
		if len(contractsForUpgrade) == 0 {
			return nil, errors.Wrapf(err, "Invalid metadata: unable to find th API Version of Cluster API (contract) supported by the %s provider", managementGroup.CoreProvider.InstanceName())
		}

		// Creates an UpgradePlan for each contract considered for upgrades; each upgrade plans contains
		// an UpgradeItem for each provider defining the next available version with the target contract, if available.
		// e.g. v1alpha3, cluster-api --> v0.3.2, kubeadm bootstrap --> v0.3.2, aws --> v0.5.4
		// e.g. v1alpha4, cluster-api --> v0.4.1, kubeadm bootstrap --> v0.4.1, aws --> v0.6.2
		for _, contract := range contractsForUpgrade {
			upgradePlan, err := u.getUpgradePlan(managementGroup, contract)
			if err != nil {
				return nil, err
			}

			// If the upgrade plan is partial (at least one upgradeItem in the plan does not have a target version) and
			// the upgrade plan requires a change of the contract for this management group, then drop it
			// (all the provider in a management group are required to change contract at the same time).
			if upgradePlan.isPartialUpgrade() && coreUpgradeInfo.currentContract != contract {
				continue
			}

			ret = append(ret, *upgradePlan)
		}
	}

	return ret, nil
}

func (u *providerUpgrader) ApplyPlan(coreProvider clusterctlv1.Provider, contract string) error {
	log := logf.Log
	log.Info("Performing upgrade...")

	// Retrieves the management group.
	managementGroup, err := u.getManagementGroup(coreProvider)
	if err != nil {
		return err
	}

	// Gets the upgrade plan for the selected management group/API Version of Cluster API (contract).
	upgradePlan, err := u.getUpgradePlan(*managementGroup, contract)
	if err != nil {
		return err
	}

	// Do the upgrade
	return u.doUpgrade(upgradePlan)
}

func (u *providerUpgrader) ApplyCustomPlan(coreProvider clusterctlv1.Provider, upgradeItems ...UpgradeItem) error {
	log := logf.Log
	log.Info("Performing upgrade...")

	// Create a custom upgrade plan from the upgrade items, taking care of ensuring all the providers in a management
	// group are consistent with the API Version of Cluster API (contract).
	upgradePlan, err := u.createCustomPlan(coreProvider, upgradeItems)
	if err != nil {
		return err
	}

	// Do the upgrade
	return u.doUpgrade(upgradePlan)
}

// getUpgradePlan returns the upgrade plan for a specific managementGroup/contract
// NB. this function is used both for upgrade plan and upgrade apply.
func (u *providerUpgrader) getUpgradePlan(managementGroup ManagementGroup, contract string) (*UpgradePlan, error) {
	upgradeItems := []UpgradeItem{}
	for _, provider := range managementGroup.Providers {
		// Gets the upgrade info for the provider.
		providerUpgradeInfo, err := u.getUpgradeInfo(provider)
		if err != nil {
			return nil, err
		}

		// Identifies the next available version with the target contract for the provider, if available.
		nextVersion := providerUpgradeInfo.getLatestNextVersion(contract)

		// Append the upgrade item for the provider/with the target contract.
		upgradeItems = append(upgradeItems, UpgradeItem{
			Provider:    provider,
			NextVersion: versionTag(nextVersion),
		})
	}

	return &UpgradePlan{
		Contract:     contract,
		CoreProvider: managementGroup.CoreProvider,
		Providers:    upgradeItems,
	}, nil
}

// getManagementGroup returns the management group for a core provider.
func (u *providerUpgrader) getManagementGroup(coreProvider clusterctlv1.Provider) (*ManagementGroup, error) {
	managementGroups, err := u.providerInventory.GetManagementGroups()
	if err != nil {
		return nil, err
	}

	managementGroup := managementGroups.FindManagementGroupByProviderInstanceName(coreProvider.InstanceName())
	if managementGroup == nil {
		return nil, errors.Errorf("unable to identify %s/%s the management group", coreProvider.Namespace, coreProvider.ProviderName)
	}

	return managementGroup, nil
}

// createCustomPlan creates a custom upgrade plan from a set of upgrade items, taking care of ensuring all the providers
// in a management group are consistent with the API Version of Cluster API (contract).
func (u *providerUpgrader) createCustomPlan(coreProvider clusterctlv1.Provider, upgradeItems []UpgradeItem) (*UpgradePlan, error) {
	// Retrieves the management group.
	managementGroup, err := u.getManagementGroup(coreProvider)
	if err != nil {
		return nil, err
	}

	// Gets the API Version of Cluster API (contract).
	// The this is required to ensure all the providers in a management group are consistent with the contract supported by the core provider.
	// e.g if the core provider is v1alpha3, all the provider in the same management group should be v1alpha3 as well.

	// The target contract is derived from the current version of the core provider, or, if the core provider is included in the upgrade list,
	// from its target version.
	targetCoreProviderVersion := managementGroup.CoreProvider.Version
	for _, providerToUpgrade := range upgradeItems {
		if providerToUpgrade.InstanceName() == managementGroup.CoreProvider.InstanceName() {
			targetCoreProviderVersion = providerToUpgrade.NextVersion
			break
		}
	}

	targetContract, err := u.getProviderContractByVersion(managementGroup.CoreProvider, targetCoreProviderVersion)
	if err != nil {
		return nil, err
	}

	// Builds the custom upgrade plan, by adding all the upgrade items after checking consistency with the targetContract.
	upgradeInstanceNames := sets.NewString()
	upgradePlan := &UpgradePlan{
		CoreProvider: managementGroup.CoreProvider,
		Contract:     targetContract,
	}

	for _, upgradeItem := range upgradeItems {
		// Match the upgrade item with the corresponding provider in the management group
		provider := managementGroup.GetProviderByInstanceName(upgradeItem.InstanceName())
		if provider == nil {
			return nil, errors.Errorf("unable to complete that upgrade: the provider %s in not part of the %s management group", upgradeItem.InstanceName(), coreProvider.InstanceName())
		}

		// Retrieves the contract that is supported by the target version of the provider.
		contract, err := u.getProviderContractByVersion(*provider, upgradeItem.NextVersion)
		if err != nil {
			return nil, err
		}

		if contract != targetContract {
			return nil, errors.Errorf("unable to complete that upgrade: the target version for the provider %s supports the %s API Version of Cluster API (contract), while the management group is using %s", upgradeItem.InstanceName(), contract, targetContract)
		}

		// Migrate the additional provider attributes to the upgrade item
		// such as watching namespace.
		upgradeItem.WatchedNamespace = provider.WatchedNamespace

		upgradePlan.Providers = append(upgradePlan.Providers, upgradeItem)
		upgradeInstanceNames.Insert(upgradeItem.InstanceName())
	}

	// Before doing upgrades, checks if other providers in the management group are lagging behind the target contract.
	for _, provider := range managementGroup.Providers {
		// skip providers already included in the upgrade plan
		if upgradeInstanceNames.Has(provider.InstanceName()) {
			continue
		}

		// Retrieves the contract that is supported by the current version of the provider.
		contract, err := u.getProviderContractByVersion(provider, provider.Version)
		if err != nil {
			return nil, err
		}

		if contract != targetContract {
			return nil, errors.Errorf("unable to complete that upgrade: the provider %s supports the %s API Version of Cluster API (contract), while the management group is being updated to %s. Please include the %[1]s provider in the upgrade", provider.InstanceName(), contract, targetContract)
		}
	}
	return upgradePlan, nil
}

// getProviderContractByVersion returns the contract that a provider will support if updated to the given target version.
func (u *providerUpgrader) getProviderContractByVersion(provider clusterctlv1.Provider, targetVersion string) (string, error) {
	targetSemVersion, err := version.ParseSemantic(targetVersion)
	if err != nil {
		return "", errors.Wrapf(err, "failed to parse target version for the %s provider", provider.InstanceName())
	}

	// Gets the metadata for the core Provider
	upgradeInfo, err := u.getUpgradeInfo(provider)
	if err != nil {
		return "", err
	}

	releaseSeries := upgradeInfo.metadata.GetReleaseSeriesForVersion(targetSemVersion)
	if releaseSeries == nil {
		return "", errors.Errorf("invalid target version: version %s for the provider %s does not match any release series", targetVersion, provider.InstanceName())
	}
	return releaseSeries.Contract, nil
}

// getUpgradeComponents returns the provider components for the selected target version.
func (u *providerUpgrader) getUpgradeComponents(provider UpgradeItem) (repository.Components, error) {
	configRepository, err := u.configClient.Providers().Get(provider.ProviderName, provider.GetProviderType())
	if err != nil {
		return nil, err
	}

	providerRepository, err := u.repositoryClientFactory(configRepository, u.configClient)
	if err != nil {
		return nil, err
	}

	options := repository.ComponentsOptions{
		Version:           provider.NextVersion,
		TargetNamespace:   provider.Namespace,
		WatchingNamespace: provider.WatchedNamespace,
	}
	components, err := providerRepository.Components().Get(options)
	if err != nil {
		return nil, err
	}
	return components, nil
}

func (u *providerUpgrader) doUpgrade(upgradePlan *UpgradePlan) error {
	for _, upgradeItem := range upgradePlan.Providers {
		// If there is not a specified next version, skip it (we are already up-to-date).
		if upgradeItem.NextVersion == "" {
			continue
		}

		// Gets the provider components for the target version.
		components, err := u.getUpgradeComponents(upgradeItem)
		if err != nil {
			return err
		}

		// Delete the provider, preserving CRD and namespace.
		if err := u.providerComponents.Delete(DeleteOptions{
			Provider:         upgradeItem.Provider,
			IncludeNamespace: false,
			IncludeCRDs:      false,
		}); err != nil {
			return err
		}

		// Install the new version of the provider components.
		if err := installComponentsAndUpdateInventory(components, u.providerComponents, u.providerInventory); err != nil {
			return err
		}
	}
	return nil
}

func newProviderUpgrader(configClient config.Client, repositoryClientFactory RepositoryClientFactory, providerInventory InventoryClient, providerComponents ComponentsClient) *providerUpgrader {
	return &providerUpgrader{
		configClient:            configClient,
		repositoryClientFactory: repositoryClientFactory,
		providerInventory:       providerInventory,
		providerComponents:      providerComponents,
	}
}
