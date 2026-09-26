# Cluster API Provider GOMI

Experimental Infrastructure Provider, built as an independent Go module and
container. It runs on the management cluster and uses GOMI's REST API, without
importing server internals or accessing its database.

## Scope

- Cluster API **v1.14.2**, **v1beta2 contract**; provider CRDs use v1alpha1.
- `GomiCluster`, `GomiMachine`, `GomiMachineTemplate`.
- Cloud-image VMs and explicitly enrolled physical hosts with the kubeadm bootstrap/control-plane providers.
- Create, observe, delete, retry, pause and rediscovery after `clusterctl move`.
- OS-neutral cloud-config transport; authenticated server integration tests cover
  Ubuntu and Fedora. Arbitrary distro images are not automatically Kubernetes-ready.

Ignition, image building, load-balancer management, CNI installation and ClusterClass
are not implemented. Deployment errors surface in Ready; replacement/remediation
is handled by CAPI and optionally a MachineHealthCheck. Physical hosts remain
allocated until OS cleanup completes; failures require recovery before reuse.

## Prerequisites

1. Management Kubernetes with CAPI v1.14.2 and kubeadm providers installed.
2. Reachable GOMI built with protected VM seed support (`GET /api/v1/capabilities`
   reports `vmSeedTemplates: true`), a ready hypervisor, bridge, subnet and DHCP
   service. Upgrade GOMI before running this provider; older servers are rejected
   before bootstrap secrets are transmitted.
3. A GOMI **qcow2 cloud image** with cloud-init, kubelet, kubeadm and a CRI runtime
   matching the requested Kubernetes version. Configure kernel/sysctl/cgroup/swap
   requirements in the image. A stock distro cloud image is insufficient.
4. An operator service-account token in a same-namespace Secret (`token` key).
   Use TLS outside an isolated lab. Restrict access to GomiClusters and Secrets.
5. A stable external control-plane endpoint. **This provider does not create or
   update a load balancer.** Register control-plane VM IPs with it, including
   replacements, and remove deleted targets. Bootstrap may wait for this step.
   The sample uses DHCP so rolling replacements do not reuse a static IP.

## Build and install

From this directory, with Go 1.26 and kubectl:

```sh
make generate test build
make integration
make docker-build IMG=your-registry.example/capgomi:dev
docker push your-registry.example/capgomi:dev
make manifests IMG=your-registry.example/capgomi:dev
kubectl apply -f dist/infrastructure-components.yaml
```

Container builds default to `linux/amd64`; set `PLATFORM=linux/arm64` for an
ARM management cluster. Alternatively load the image into an isolated local cluster. No public image or
release is assumed to exist. Manifests include CRDs, RBAC, a non-root manager,
leader election and health probes. `--namespace` restricts reconciliation; empty
means all namespaces. Credentials are reread each reconciliation for rotation.
TLS uses system trust; there is no insecure verification bypass.

`make integration` downloads versioned CAPI CRDs and envtest binaries and starts
isolated local kube-apiserver/etcd processes. It **does not use your kubeconfig**.
It tests real Kubernetes schema/CEL/status/finalizer behavior and real GOMI HTTP
handlers. Guest completion and hypervisor teardown are simulated: this is not
an actual guest-boot E2E test. Integration dependencies are in a separate module.

## Create a workload cluster

Render `samples/cluster-template.yaml` with `clusterctl generate cluster` after
configuring release assets, or use envsubst supporting `${VAR:=default}` syntax.
Keep rendered manifests out of Git/logs: the sample includes a credentials Secret.

| Variable | Meaning |
| --- | --- |
| `CLUSTER_NAME`, `NAMESPACE` | Cluster name and existing namespace |
| `KUBERNETES_VERSION` | Version installed in the prepared image |
| `GOMI_ENDPOINT` | Origin URL, e.g. `https://gomi.example.net`, no `/api/v1` |
| `GOMI_TOKEN` | Operator service-account token |
| `GOMI_HYPERVISOR`, `GOMI_OS_IMAGE` | Registered hypervisor and prepared image IDs |
| `GOMI_BRIDGE`, `GOMI_SUBNET` | Bridge and subnet IDs |
| `CONTROL_PLANE_ENDPOINT` | External LB hostname/IP |
| `CONTROL_PLANE_MACHINE_COUNT`, `WORKER_MACHINE_COUNT` | Default 1 each |

The sample configures kubelet provider ID as `gomi:///{{ v1.local_hostname }}`
for init and join. CABPK emits Jinja-enabled cloud-config; GOMI uses the persisted
instance ID as the hostname. Preserve these settings so CAPI can associate the
Node and Machine. Install a CNI compatible with the sample pod CIDR
`192.168.0.0/16`; change the CIDR if it overlaps your network.

```sh
kubectl get clusters,machines,gomiclusters,gomimachines -n "$NAMESPACE"
kubectl describe gomimachine -n "$NAMESPACE" <name>
clusterctl get kubeconfig "$CLUSTER_NAME" -n "$NAMESPACE" > workload.kubeconfig
kubectl --kubeconfig workload.kubeconfig get nodes
```

Delete the **CAPI Cluster or Machine**, allowing CAPI to drain nodes before VM
removal. Keep GOMI and credentials available until GomiMachines finish deletion.
Failed deletion retains its finalizer and bootstrap data for retry; do not remove
the finalizer without first recovering or cleaning external resources.

## Bootstrap confidentiality

CAPI cloud-config can contain cluster CA private keys. Templates use
`deliveryMode: vm-seed`: GOMI renders their NoCloud seed inside the server and
uploads the ISO over the configured hypervisor connection. Public PXE boot and
NoCloud endpoints reject these targets during and after provisioning, including
requests with a completion token. There is no bearer-token-based public delivery
fallback. Normal templates keep their existing PXE behavior.

Protected template content is available only to authenticated operators/admins;
viewer lists redact it. The delivery restriction survives database migrations,
restarts and edits from clients that omit the field. Templates remain until VM
deletion for ownership/retry handling; internal rendering stops when provisioning
ends. The guest disk and attached seed contain sensitive material: protect GOMI's
database/backups, the hypervisor connection, storage and administrator credentials.

## Identity and recovery

`spec.instanceID` and the finalizer are persisted before external writes. VM and
cloud-init template names use this stable ID. Existing VMs must reference the
owned template; template content is checked before initial VM creation. Do not
manually edit provider-owned resources. IDs and credentials are trusted admin
inputs, not a tenant security boundary.

`clusterctl move` replaces Kubernetes UIDs and drops status. Identity in spec and
GOMI observations allow reconstruction. Initialization is restored independently
of power state; a stopped VM stays initialized while Ready is false. The sample credentials Secret uses the
explicit `clusterctl.cluster.x-k8s.io/move` label. Bootstrap Secrets are only needed
before creation; an existing VM can be observed/deleted after the bootstrap Secret
is gone. Missing VMs with saved provider IDs are reported rather than recreated.

## Releases

GOMI `v*` releases include an amd64 Docker image archive, provider manifests,
metadata, sample and `provider-checksums.txt`. The manifest uses `capgomi:<tag>`;
there is no public container registry image. Import the archive into every
management-cluster node's container runtime (for kind, use `kind load image-archive`),
or load, tag and push it to your registry and update the Deployment image before
installation. Loading into Docker on your workstation alone does not make the
image available to a remote Kubernetes cluster.

```sh
TAG=v0.0.33
gh release download "$TAG" --repo sugaf1204/gomi --pattern 'cluster-api-provider-gomi_*' --pattern 'provider-checksums.txt' --pattern '*.yaml' --pattern 'provider-README.md'
sha256sum -c provider-checksums.txt
docker load -i "cluster-api-provider-gomi_${TAG}_linux_amd64.tar.gz"
# Example for a local kind management cluster:
kind load docker-image "docker.io/library/capgomi:${TAG}" --name <management-cluster>
kubectl apply -f infrastructure-components.yaml
```


`make manifests IMG=...` outputs components, metadata and cluster-template YAML
under `dist/`. Publish these together on a SemVer GitHub release. Initial metadata
maps 0.0.x to v1beta2; update it for additional minor release series. For published
assets, configure clusterctl with a custom InfrastructureProvider named `gomi`
and the release's components URL. No built-in provider registration is included.

## BareMetal enrollment and lifecycle

Use `kind: BareMetal` with `bareMetal.pool` and `bareMetal.osImageRef` in the
GomiMachineTemplate instead of `virtualMachine`. GOMI must report
`bareMetalSealedBootstrap: true`. An administrator enrolls each existing physical
machine using `PUT /api/v1/bare-metal-hosts/<name>` with an explicit whole-disk `targetDisk`, a `pool` and trusted RSA
`publicKey` in PEM SubjectPublicKeyInfo format. Obtain it through a verified SSH
connection or another trusted channel, for example `ssh-keygen -e -m PKCS8 -f
/etc/ssh/ssh_host_rsa_key.pub` on the host. Never send a private key to this API.

The matching unencrypted RSA host private key must already be on the selected
installation disk, in `/etc/ssh/ssh_host_rsa_key`, on an ext-family or XFS filesystem.
For a blank host, provision/enroll its identity through a trusted initial setup
first. Unsupported storage (including encrypted root without an unlock path) fails
before partitioning. Keep an encrypted/offline identity backup for recovery from a
power loss between disk erasure and key restoration. The temporary preservation
copy is in RAM and cannot survive loss of power.

The provider seals CABPK cloud-config using RSA-OAEP-SHA256 and AES-256-GCM, binding
it to the host and stable claim owner. GOMI persists ciphertext, never a plaintext
physical-bootstrap template. The updated boot environment preserves and verifies
the host key, decrypts bootstrap before disk partitioning, installs the SquashFS
rootfs, restores host keys and merges bootstrap into the local NoCloud seed. CA
keys and join tokens are not served as public PXE user-data. Enrollment encryption
protects confidentiality; it does not authenticate unsigned PXE boot code. Keep the
provisioning network trusted and isolate it from untrusted clients.

Only amd64 Ubuntu, Debian and Fedora SquashFS deployment paths with ext4 or XFS
target roots are currently accepted. Missing/mismatched image architecture and
btrfs target roots are rejected before deployment, so later identity preservation
and cleanup remain supported. Manual-power and hypervisor-role hosts cannot be
enrolled.
The image and/or bootstrap commands must install Kubernetes/CRI prerequisites for
the selected OS/version. The provider does not turn an arbitrary image into a
Kubernetes node automatically. Use `gomi:///{{ v1.instance_id }}` for kubelet's
provider ID: physical metadata retains the host name but its instance ID is the
stable CAPI claim ID.

Pool acquisition and deployment commit are atomic in GOMI's SQL store. A repeat or
lost response observes the existing claim instead of starting another install.
Normal machine mutation APIs are blocked for enrolled hosts. A deletion during
installation waits rather than rebooting an installer holding its key in RAM.
Once installation completes, deletion starts a separate OS reset without CABPK
data and keeps the finalizer until the reset completes. It preserves hardware
inventory and the enrollment identity. It does not promise forensic erasure of
old flash blocks. A failed physical deployment retains its claim for recovery;
there is no automatic endless reinstall loop.

This is reprovisioning, not adoption of an existing Kubernetes cluster. Back up
etcd, application data and host identities before enabling the physical pool.
Three hosts with three control planes have no spare surge capacity: use a CAPI
rollout strategy compatible with the available pool and preserve etcd quorum.

For crypto/seed interoperability checks, install `bootenv/tests/requirements.txt`
in a virtual environment, set `GOMI_BOOTSTRAP_TEST_PYTHON` to its Python executable,
and run the provider tests plus `python -m unittest discover -s bootenv/tests` from
the repository root. `make integration` includes real Kubernetes CRD validation,
SQL allocation and the GOMI HTTP lifecycle; physical boot still needs live testing.
