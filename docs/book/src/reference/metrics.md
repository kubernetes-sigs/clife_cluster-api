## Metrics

By default, controller-runtime builds a global Prometheus registry and
publishes a collection of performance metrics for each controller.

### Protecting the Metrics

These metrics are protected by [kube-auth-proxy](https://github.com/brancz/kube-rbac-proxy)
by default.

You will need to grant permissions to your Prometheus server so that it can
scrape the protected metrics. To achieve that, you can create a `ClusterRole` and a 
`ClusterRoleBinding` to bind to the service account that your Prometheus server uses.

Create a manifest named `capi-metrics-reader-clusterrole.yaml` with the following content:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: capi-metrics-reader
rules:
- nonResourceURLs: ["/metrics"]
  verbs: ["get"]
```

and apply the `ClusterRole` with

```bash
kubectl apply -f capi-metrics-reader-clusterrole.yaml
```

You can run the following kubectl command to create a `ClusterRoleBinding` and grant access on the `/metrics` endpoint to your Prometheus instance (`<namespace>` must be the namespace where your Prometheus instance is running. `<service-account-name>` must be the service account name which is configured in your Prometheus instance).

```bash
kubectl create clusterrolebinding capi-metrics-reader --clusterrole=capi-metrics-reader --serviceaccount=<namespace>:<service-account-name>
```

### Scraping the Metrics with Prometheus

To scrape metrics, your Prometheus instance needs at least the following [`kubernetes_sd_config`](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#kubernetes_sd_config) section.


```yaml
      # This job is primarily used for Pods with multiple metrics port.
      # Per port one service is created and scraped.
      - job_name: 'kubernetes-service-endpoints'
        tls_config:
          # if service endpoints use their own CA (e.g. via cert-manager) which aren't
          # signed by the cluster-internal CA we must skip the cert validation
          insecure_skip_verify: true
        bearer_token_file: /var/run/secrets/kubernetes.io/serviceaccount/token
        kubernetes_sd_configs:
          - role: endpoints
        relabel_configs:
          - source_labels: [__meta_kubernetes_service_annotation_prometheus_io_scrape]
            action: keep
            regex: true
          - source_labels: [__meta_kubernetes_service_annotation_prometheus_io_scheme]
            action: replace
            target_label: __scheme__
            regex: (https?)
          - source_labels: [__meta_kubernetes_service_annotation_prometheus_io_path]
            action: replace
            target_label: __metrics_path__
            regex: (.+)
          - source_labels: [__address__, __meta_kubernetes_service_annotation_prometheus_io_port]
            action: replace
            target_label: __address__
            regex: ([^:]+)(?::\d+)?;(\d+)
            replacement: $1:$2
          - action: labelmap
            regex: __meta_kubernetes_service_label_(.+)
```

You are now able to check for metrics in your Prometheus instance. To verify, you could search with e.g. `{namespace="capi-system"}` to get all metrics from components running in the `capi-system` namespace.
