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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CasdoorOrganizationSpec describes an organization. Fields other than casdoorRef, name,
// deletionPolicy and extraFields keep the names of the Casdoor API, and only the fields that
// are set are written, so values changed in the Casdoor console for other fields are kept.
type CasdoorOrganizationSpec struct {
	CasdoorRef CasdoorReference `json:"casdoorRef"`

	// Name of the organization in Casdoor. Defaults to the name of this object.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="name is immutable"
	// +optional
	Name string `json:"name,omitempty"`

	// DeletionPolicy decides whether deleting this object deletes the organization in Casdoor.
	// +kubebuilder:validation:Enum=Delete;Retain
	// +kubebuilder:default=Retain
	// +optional
	DeletionPolicy string `json:"deletionPolicy,omitempty"`

	// +optional
	DisplayName string `json:"displayName,omitempty"`

	// +optional
	WebsiteUrl string `json:"websiteUrl,omitempty"`

	// +optional
	Logo string `json:"logo,omitempty"`

	// +optional
	Favicon string `json:"favicon,omitempty"`

	// +kubebuilder:validation:Enum=plain;salt;sha512-salt;md5-salt;bcrypt;pbkdf2-salt;argon2id;pbkdf2-django
	// +optional
	PasswordType string `json:"passwordType,omitempty"`

	// +optional
	DefaultApplication string `json:"defaultApplication,omitempty"`

	// +optional
	Languages []string `json:"languages,omitempty"`

	// +optional
	CountryCodes []string `json:"countryCodes,omitempty"`

	// +optional
	Tags []string `json:"tags,omitempty"`

	// +optional
	EnableSoftDeletion *bool `json:"enableSoftDeletion,omitempty"`

	// ExtraFields sets any other field of the Casdoor organization, e.g. mfaItems or accountItems.
	// +optional
	ExtraFields map[string]apiextensionsv1.JSON `json:"extraFields,omitempty"`
}

type CasdoorOrganizationStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=casdoororg
// +kubebuilder:printcolumn:name="Casdoor",type=string,JSONPath=`.spec.casdoorRef.name`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// CasdoorOrganization is an organization in a Casdoor server.
type CasdoorOrganization struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CasdoorOrganizationSpec   `json:"spec"`
	Status CasdoorOrganizationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

type CasdoorOrganizationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CasdoorOrganization `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CasdoorOrganization{}, &CasdoorOrganizationList{})
}
