# Cluster API workspace

The **Compute → Cluster API** screen connects the GOMI server to one explicit
management-cluster kubeconfig. Credentials stay on the server. The UI does not
read browser-supplied kubeconfigs or proxy arbitrary Kubernetes requests.

## Connection

Add to the existing GOMI server configuration:

```yaml
cluster_api:
  kubeconfig: /etc/gomi/management.kubeconfig
  context: gomi-management
  namespace: default
```

The kubeconfig path enables the integration. Without it, GOMI remains usable
and the workspace shows connection instructions. An empty namespace allows all
namespaces accessible to the configured credentials. Set a namespace to restrict
both reads and writes. An empty context selects the explicit file's current
context. Ambient `KUBECONFIG` and in-cluster credentials are never used.

Environment overrides:

- `GOMI_CLUSTER_API_KUBECONFIG`
- `GOMI_CLUSTER_API_CONTEXT`
- `GOMI_CLUSTER_API_NAMESPACE`

Equivalent flags are `--cluster-api-kubeconfig`, `--cluster-api-context`, and
`--cluster-api-namespace`. Precedence is YAML, environment, then flags.
For these three environment variables, a set-but-empty value overrides YAML:
an empty kubeconfig disables the integration, an empty context selects the
file's current context, and an empty namespace selects all accessible namespaces.
The kubeconfig is trusted server configuration and can use client-go credential
plugins; install any required authentication plugin on the server. Use a
dedicated identity and a restricted kubeconfig rather than an administrator's
personal configuration. Configuration changes require a GOMI server restart.

## Resources and operations

The workspace shares the VM/Machine index-and-detail layout. The cluster index
keeps connection scope, filtering, and observation time together. The selected
cluster provides Overview, Machines, and Conditions sections. Overview shows
control-plane and worker ready/desired counts and readiness reasons; Machines
uses aligned rows with expandable infrastructure details. Raw conditions remain
in their own section. Ready, not-ready, failed, paused, and unknown states use
the existing GOMI status colors; missing readiness is not treated as failure.

- Cluster list with namespace filter, pause state, deletion state, and conditions.
- Cluster detail with infrastructure reference, KubeadmControlPlane resources,
  MachineDeployments, and Machines (node name, provider ID, version, conditions).
- Worker desired-count changes, including zero, for standalone MachineDeployments.
- Pause/resume of Cluster reconciliation, including removal of a legacy pause
  annotation on resume. Resuming can execute previously pending changes.

Viewer access is read-only. Operators and administrators can change resources.
Each change requires confirmation of the namespace/name, includes the observed
UID and resourceVersion, and records an audit event. Refresh and review again on
conflict. Scale-down drains and deletes worker nodes and infrastructure through
CAPI; the UI does not select individual nodes. Scale-up consumes infrastructure
capacity. Changes are desired-state updates, not a declaration that CAPI has
finished provisioning. Inspect readiness and conditions after applying.

The workspace polls every 15 seconds while visible and supports manual refresh.
An unsuccessful refresh preserves the last observation, marks it stale, and
hides mutation controls. Resources from different namespaces are never grouped
together. Reads use Kubernetes pagination; served v1beta2 is preferred with a
v1beta1 fallback per resource. A missing KubeadmControlPlane CRD is allowed; core
CAPI CRDs and read permissions are required. Other control-plane providers and
MachinePools are not shown in this first implementation.

Worker count changes are rejected for deleting or paused resources, topology
managed clusters, and autoscaler managed MachineDeployments. Change topology
replicas in the Cluster topology and autoscaler limits through their owning
configuration. This UI intentionally does not offer generic YAML editing,
cluster creation/deletion, Kubernetes upgrades, or control-plane scaling.

## Kubernetes RBAC

For a namespace-scoped connection, bind a Role like the following to the
management identity. Substitute the namespace. Use a ClusterRole and
ClusterRoleBinding only when all-namespace access is intended.

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: gomi-cluster-api-ui
  namespace: default
rules:
  - apiGroups: [cluster.x-k8s.io]
    resources: [clusters, machinedeployments, machines]
    verbs: [get, list]
  - apiGroups: [cluster.x-k8s.io]
    resources: [clusters, machinedeployments]
    verbs: [patch]
  - apiGroups: [controlplane.cluster.x-k8s.io]
    resources: [kubeadmcontrolplanes]
    verbs: [list]
```

No Secrets, workload-cluster credentials, or node APIs are required. GOMI roles
are enforced by the GOMI API, independently of the management identity's RBAC.
The new code does not change provider reconciliation, VM or bare-metal power
control, OS deployment, firmware selection, or bootstrap paths.

CAPI behavior references:
[worker scaling](https://cluster-api.sigs.k8s.io/tasks/automated-machine-management/scaling),
[topology scaling](https://cluster-api.sigs.k8s.io/tasks/experimental-features/cluster-class/operate-cluster),
[pause annotation](https://cluster-api.sigs.k8s.io/reference/api/labels-and-annotations).
