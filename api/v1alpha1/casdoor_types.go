// Copyright 2026 The Casdoor Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	DriverMySQL    = "mysql"
	DriverPostgres = "postgres"
	DriverSQLite   = "sqlite"

	ConditionReady = "Ready"
)

// CasdoorSpec defines the desired state of a Casdoor instance.
// +kubebuilder:validation:XValidation:rule="!has(self.replicas) || self.replicas <= 1 || self.database.driver != 'sqlite'",message="sqlite supports a single replica only"
// +kubebuilder:validation:XValidation:rule="!has(self.replicas) || self.replicas <= 1 || has(self.redis)",message="redis is required to share sessions between replicas"
type CasdoorSpec struct {
	// Image is the Casdoor image. Defaults to the version the operator was released with.
	// +optional
	Image string `json:"image,omitempty"`

	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Origin is the public URL of this instance, e.g. https://door.example.com.
	// It becomes the OIDC issuer. Defaults to the ingress host when ingress is set.
	// +kubebuilder:validation:Pattern=`^https?://[^/]+$`
	// +optional
	Origin string `json:"origin,omitempty"`

	Database DatabaseSpec `json:"database"`

	// Redis stores sessions. Required when replicas is greater than 1.
	// +optional
	Redis *RedisSpec `json:"redis,omitempty"`

	// InitAdminPasswordSecretRef points to the password of built-in/admin, applied when
	// Casdoor initializes an empty database. Without it Casdoor asks for a password on first visit.
	// +optional
	InitAdminPasswordSecretRef *corev1.SecretKeySelector `json:"initAdminPasswordSecretRef,omitempty"`

	// InitData loads organizations, applications, users and so on from an init_data.json.
	// +optional
	InitData *InitDataSpec `json:"initData,omitempty"`

	// Config holds extra app.conf items, e.g. defaultLanguage or staticBaseUrl.
	// Items managed by other fields of this spec cannot be overridden here.
	// +optional
	Config map[string]string `json:"config,omitempty"`

	// Env is appended to the Casdoor container after the generated variables.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// +optional
	Service ServiceSpec `json:"service,omitempty"`

	// +optional
	Ingress *IngressSpec `json:"ingress,omitempty"`

	// +optional
	PodLabels map[string]string `json:"podLabels,omitempty"`

	// +optional
	PodAnnotations map[string]string `json:"podAnnotations,omitempty"`

	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// +optional
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
}

// DatabaseSpec describes the database Casdoor stores its data in.
// +kubebuilder:validation:XValidation:rule="self.driver == 'sqlite' || has(self.host) || has(self.dataSourceNameSecretRef)",message="host or dataSourceNameSecretRef is required for mysql and postgres"
// +kubebuilder:validation:XValidation:rule="self.driver == 'sqlite' || !has(self.storage)",message="storage is only used by sqlite"
type DatabaseSpec struct {
	// +kubebuilder:validation:Enum=mysql;postgres;sqlite
	Driver string `json:"driver"`

	// +optional
	Host string `json:"host,omitempty"`

	// Port defaults to 3306 for mysql and 5432 for postgres.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Port int32 `json:"port,omitempty"`

	// +kubebuilder:default=casdoor
	// +optional
	Name string `json:"name,omitempty"`

	// +optional
	User string `json:"user,omitempty"`

	// +optional
	PasswordSecretRef *corev1.SecretKeySelector `json:"passwordSecretRef,omitempty"`

	// SSLMode is the postgres sslmode.
	// +kubebuilder:default=disable
	// +optional
	SSLMode string `json:"sslMode,omitempty"`

	// DataSourceNameSecretRef points to a complete dataSourceName and replaces
	// host, port, user, passwordSecretRef and sslMode.
	// +optional
	DataSourceNameSecretRef *corev1.SecretKeySelector `json:"dataSourceNameSecretRef,omitempty"`

	// Create lets Casdoor create the mysql or postgres database when it does not exist.
	// +kubebuilder:default=true
	// +optional
	Create *bool `json:"create,omitempty"`

	// Storage requests a volume for the sqlite file. Without it the file lives in an emptyDir
	// and is lost when the pod is rescheduled.
	// +optional
	Storage *StorageSpec `json:"storage,omitempty"`
}

type StorageSpec struct {
	// +kubebuilder:default="1Gi"
	// +optional
	Size resource.Quantity `json:"size,omitempty"`

	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`
}

type RedisSpec struct {
	// Endpoint is host:port. Several addresses separated by ";" turn on Redis Cluster mode.
	// +kubebuilder:validation:MinLength=1
	Endpoint string `json:"endpoint"`

	// +optional
	PasswordSecretRef *corev1.SecretKeySelector `json:"passwordSecretRef,omitempty"`

	// +kubebuilder:validation:Minimum=0
	// +optional
	DB int32 `json:"db,omitempty"`
}

type InitDataSpec struct {
	// SecretRef points to the init_data.json content.
	SecretRef corev1.SecretKeySelector `json:"secretRef"`

	// NewOnly skips objects that already exist instead of overwriting them.
	// +kubebuilder:default=true
	// +optional
	NewOnly *bool `json:"newOnly,omitempty"`
}

type ServiceSpec struct {
	// +kubebuilder:default=ClusterIP
	// +kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer
	// +optional
	Type corev1.ServiceType `json:"type,omitempty"`

	// +kubebuilder:default=8000
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Port int32 `json:"port,omitempty"`

	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

type IngressSpec struct {
	// +optional
	ClassName *string `json:"className,omitempty"`

	// +kubebuilder:validation:MinLength=1
	Host string `json:"host"`

	// TLSSecretName turns on TLS for the host.
	// +optional
	TLSSecretName string `json:"tlsSecretName,omitempty"`

	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// CasdoorStatus defines the observed state of a Casdoor instance.
type CasdoorStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// Selector is the pod label selector, used by the scale subresource.
	// +optional
	Selector string `json:"selector,omitempty"`

	// Image is the image the deployment currently runs.
	// +optional
	Image string `json:"image,omitempty"`

	// URL is where the instance is reachable: the origin, or the in-cluster service address.
	// +optional
	URL string `json:"url,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas,selectorpath=.status.selector
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Replicas",type=string,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=`.status.image`
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=`.status.url`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Casdoor is a Casdoor instance.
type Casdoor struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CasdoorSpec   `json:"spec"`
	Status CasdoorStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type CasdoorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Casdoor `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Casdoor{}, &CasdoorList{})
}
