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
)

const (
	DeletionPolicyDelete = "Delete"
	DeletionPolicyRetain = "Retain"

	ConditionAPIReady = "APIReady"
)

// CasdoorReference selects the Casdoor server an object is created in.
// +kubebuilder:validation:XValidation:rule="has(self.name) != has(self.external)",message="exactly one of name and external must be set"
type CasdoorReference struct {
	// Name of a Casdoor object in the same namespace.
	// +optional
	Name string `json:"name,omitempty"`

	// External is a Casdoor server not managed by this operator.
	// +optional
	External *ExternalCasdoor `json:"external,omitempty"`
}

type ExternalCasdoor struct {
	// Endpoint is the URL of the server, e.g. https://door.example.com.
	// +kubebuilder:validation:Pattern=`^https?://.+`
	Endpoint string `json:"endpoint"`

	// CredentialsSecretRef points to a Secret with the clientId and clientSecret keys of an
	// application in the built-in organization, which can manage every organization.
	CredentialsSecretRef corev1.LocalObjectReference `json:"credentialsSecretRef"`
}
