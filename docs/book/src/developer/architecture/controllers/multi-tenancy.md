# Multi tenancy

Multi tenancy in Cluster API defines the capability of an infrastructure provider to manage multiple sets of
credentials, each one of them corresponding to an infrastructure tenant.

## Contract

In order to support multi tenancy, the following rule applies:

- Infrastructure providers MUST be able to manage different sets of credentials (if any)
- Providers SHOULD deploy and run any kind of webhook (validation, admission, conversion)
  following Cluster API codebase best practices for the same release.
- Providers MUST create and publish a `{type}-component.yaml` accordingly.
- Providers MUST implement a reference to the identity by including `InfraClusterResourceIdentitySpec`
  in the relevant resource, e.g.

  ```go
  type InfraCluster struct {
    clusterv1.InfraClusterResourceIdentitySpec `json:",inline"`
  }
  ```

- A Namespace field MUST never be added to the ProviderIdentityReference type to avoid crossing namespace boundaries.

- Where identity types use private key material, CRs MUST implement a `secretRef` on their spec of type string and only
  read secrets from the same namespace as the CR for namespaced scope resources OR the controller namespace for
  cluster-scoped resources.

- Providers MAY support `Secret` as a top-level supported identity type (either on top of custom resources or instead of), but only from the same namespace as the owning `InfraCluster` CR.

- Providers MAY support the use of `identityRef` in other low-level resources, such as Load Balancers.

## Supported RBAC Models

Providers MAY support any combination of cluster-scoped or namespace-scoped resources as follows:

### Cluster-scoped global resources for delegated access

In a common use for multi-tenancy, a cloud admin will want to set up a range of identities, and then delegate them to
individual teams. This is best done using global resources to prevent repetition.

#### Cluster-scoped Contract

- Cluster scoped resources MUST be named with `<Provider>Cluster<Type>Identity`. Examples:
  - `FabrikamClusterRoleIdentity`
  - `FabrikamClusterStaticIdentity`

- Where identity types use private key material, CRs MUST implement a `secretRef` on their spec of type string and only
  read secrets from the controller namespace.

- Cluster scoped resources MUST be delegated using a label selector, present on the spec as:

```go
type InfraClusterIdentity struct {
  // ADDITIONAL INFRASTRUCTURE SPECIFIC IDENTITY FIELDS//


  clusterv1.InfraClusterScopedResourceIdentityCommonSpec `json:",inline"`
}
```

- Providers MAY support an additional `NamespaceList` string slice in the `AllowedNamespaces` struct, included in
  clusterv1.InfraClusterScopedResourceIdentityCommonSpec.
- Providers SHOULD use the webhook validations provided in the clusterv1 package for
  clusterv1.InfraClusterScopedResourceIdentityCommonSpec

When Cluster API no longer supports Kubernetes versions older than Kubernetes v1.21, when the NamespaceDefaultLabelName
[feature gate] transitioned to Beta, then:

  - Providers MUST remove the `NamespaceList` field.
  - Conversion webhooks MUST perform the following:
    - Translate the NamespaceList into the following match expression:
      -  `kubernetes.io/metadata.name in (<comma-separated list of namespaces from <NamespaceList>)`

<!-- TODO @randomvariable: Remove this line when this https://github.com/kubernetes-sigs/cluster-api/issues/4941
is resolved -->
These behaviours will be encoded in utility functions in the Cluster API repository at a later date.

### Namespaced scoped resources

Namespaced scoped resources are useful most particularly when you want to allow developers to provision clusters on
their own accounts, but may not be suitable for every use case.

#### Namespace-scoped contract

- Namespace scoped resources MUST be named `<Provider><Type>Identity`, e.g.:
  - `ContosoCloudRoleIdentity`
  - `ContosoCloudStaticIdentity`
- Where identity types use private key material, CRs MUST implement a `secretRef` on their spec of type string and only
  read secrets from the same namespace as the CR.

[feature gate]: https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/
