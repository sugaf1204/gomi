// +kubebuilder:object:generate=true
// +groupName=infrastructure.cluster.x-k8s.io
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var GroupVersion = schema.GroupVersion{Group: "infrastructure.cluster.x-k8s.io", Version: "v1alpha1"}
var SchemeBuilder = runtime.NewSchemeBuilder(func(s *runtime.Scheme) error {
	s.AddKnownTypes(GroupVersion, &GomiCluster{}, &GomiClusterList{}, &GomiMachine{}, &GomiMachineList{}, &GomiMachineTemplate{}, &GomiMachineTemplateList{})
	metav1.AddToGroupVersion(s, GroupVersion)
	return nil
})
var AddToScheme = SchemeBuilder.AddToScheme

type Endpoint struct {
	// +kubebuilder:validation:MinLength=1
	Host string `json:"host"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`
}
type SecretReference struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}
type GomiClusterSpec struct {
	// Server is the GOMI origin URL, without /api/v1.
	// +kubebuilder:validation:Pattern=`^https?://[^/]+/?$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="server is immutable"
	Server string `json:"server"`
	// CredentialsRef references a Secret in this namespace with a token key.
	CredentialsRef       SecretReference `json:"credentialsRef"`
	ControlPlaneEndpoint Endpoint        `json:"controlPlaneEndpoint"`
}
type Initialization struct {
	Provisioned *bool `json:"provisioned,omitempty"`
}
type GomiClusterStatus struct {
	Initialization Initialization `json:"initialization,omitempty"`
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories=cluster-api
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1alpha1"
type GomiCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              GomiClusterSpec   `json:"spec"`
	Status            GomiClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type GomiClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GomiCluster `json:"items"`
}
type Resources struct {
	// +kubebuilder:validation:Minimum=1
	CPUCores int `json:"cpuCores"`
	// +kubebuilder:validation:Minimum=512
	MemoryMB int64 `json:"memoryMB"`
	// +kubebuilder:validation:Minimum=1
	DiskGB int `json:"diskGB"`
}
type NetworkInterface struct {
	Name      string `json:"name"`
	Bridge    string `json:"bridge,omitempty"`
	IPAddress string `json:"ipAddress,omitempty"`
}
type VirtualMachineSpec struct {
	HypervisorRef string `json:"hypervisorRef,omitempty"`
	// OSImageRef must reference an image with cloud-init, container runtime,
	// kubelet and kubeadm installed for the requested Kubernetes version.
	// +kubebuilder:validation:MinLength=1
	OSImageRef string             `json:"osImageRef"`
	Resources  Resources          `json:"resources"`
	Network    []NetworkInterface `json:"network,omitempty"`
	// +kubebuilder:validation:Enum=dhcp;static
	IPAssignment string   `json:"ipAssignment,omitempty"`
	SubnetRef    string   `json:"subnetRef,omitempty"`
	SSHKeyRefs   []string `json:"sshKeyRefs,omitempty"`
}

// ConfigMapKeyReference selects one key from an immutable ConfigMap in the
// GomiMachine namespace.
type ConfigMapKeyReference struct {
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// +kubebuilder:validation:MinLength=1
	Key string `json:"key"`
}

// BareMetalSpec selects a pool enrolled explicitly through the GOMI API.
type BareMetalSpec struct {
	// +kubebuilder:validation:MinLength=1
	Pool string `json:"pool"`
	// OSImageRef must be a prepared bare-metal image with cloud-init. Kubernetes
	// node packages may be installed by CloudInitConfigRef.
	// +kubebuilder:validation:MinLength=1
	OSImageRef string `json:"osImageRef"`
	// CloudInitConfigRef references declarative, non-secret OS preparation data.
	// GOMI applies it before CABPK's sealed kubeadm bootstrap. The ConfigMap must
	// be immutable so a moved Machine can verify the same provisioning input.
	CloudInitConfigRef *ConfigMapKeyReference `json:"cloudInitConfigRef,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="!has(oldSelf.instanceID) || has(self.instanceID)",message="instanceID cannot be removed"
// +kubebuilder:validation:XValidation:rule="(self.kind == 'BareMetal') == has(self.bareMetal)",message="BareMetal requires bareMetal and no virtualMachine"
// +kubebuilder:validation:XValidation:rule="(self.kind == 'VirtualMachine') == has(self.virtualMachine)",message="VirtualMachine requires virtualMachine and no bareMetal"
// +kubebuilder:validation:XValidation:rule="self.kind == oldSelf.kind",message="kind is immutable"
type GomiMachineSpec struct {
	// +kubebuilder:validation:Enum=VirtualMachine;BareMetal
	// +kubebuilder:default=VirtualMachine
	Kind string `json:"kind,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="virtualMachine is immutable; replace the Machine instead"
	VirtualMachine *VirtualMachineSpec `json:"virtualMachine,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="bareMetal is immutable; replace the Machine instead"
	BareMetal *BareMetalSpec `json:"bareMetal,omitempty"`
	// InstanceID is persisted before provisioning and survives clusterctl move.
	// +kubebuilder:validation:Pattern=`^capi-[a-z0-9-]+$`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="instanceID is immutable"
	InstanceID string  `json:"instanceID,omitempty"`
	ProviderID *string `json:"providerID,omitempty"`
}
type Address struct {
	Type    string `json:"type"`
	Address string `json:"address"`
}
type GomiMachineStatus struct {
	Initialization Initialization `json:"initialization,omitempty"`
	Addresses      []Address      `json:"addresses,omitempty"`
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:categories=cluster-api
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1alpha1"
type GomiMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              GomiMachineSpec   `json:"spec"`
	Status            GomiMachineStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type GomiMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GomiMachine `json:"items"`
}

// +kubebuilder:validation:XValidation:rule="(self.kind == 'BareMetal') == has(self.bareMetal)",message="BareMetal requires bareMetal and no virtualMachine"
// +kubebuilder:validation:XValidation:rule="(self.kind == 'VirtualMachine') == has(self.virtualMachine)",message="VirtualMachine requires virtualMachine and no bareMetal"
type GomiMachineTemplateMachineSpec struct {
	// +kubebuilder:validation:Enum=VirtualMachine;BareMetal
	// +kubebuilder:default=VirtualMachine
	Kind           string              `json:"kind,omitempty"`
	VirtualMachine *VirtualMachineSpec `json:"virtualMachine,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="bareMetal is immutable; replace the Machine instead"
	BareMetal *BareMetalSpec `json:"bareMetal,omitempty"`
}
type GomiMachineTemplateResource struct {
	Spec GomiMachineTemplateMachineSpec `json:"spec"`
}
type GomiMachineTemplateSpec struct {
	Template GomiMachineTemplateResource `json:"template"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:categories=cluster-api
// +kubebuilder:metadata:labels="cluster.x-k8s.io/v1beta2=v1alpha1"
type GomiMachineTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="template is immutable"
	Spec GomiMachineTemplateSpec `json:"spec"`
}

// +kubebuilder:object:root=true
type GomiMachineTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GomiMachineTemplate `json:"items"`
}
