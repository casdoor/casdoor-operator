/*
Copyright 2022.

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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"strconv"
)

type CasdoorStatusEnum string

const (
	CasdoorStatusPending CasdoorStatusEnum = "Pending"
	CasdoorStatusRunning CasdoorStatusEnum = "Running"
	CasdoorStatusFailed  CasdoorStatusEnum = "Failed"
)

// CasdoorSpec defines the desired state of Casdoor
type CasdoorSpec struct {
	// items for deploy rules
	// +kubebuilder:default:=1
	Replicas *int32 `json:"replica,omitempty"`
	// +kubebuilder:default:="casbin/casdoor-all-in-one:latest"
	Image string `json:"image,omitempty"`
	// +kubebuilder:default:="IfNotPresent"
	ImagePullPolicy string `json:"imagePullPolicy,omitempty"`

	// items for `app.conf`
	AppConf map[string]string `json:"appConf,omitempty"`

	// items for `init_data.json`
	InitData string `json:"initData,omitempty"`

	// in-cluster static file server
	InClusterCDN bool `json:"inClusterCDN,omitempty"`
}

// CasdoorStatus defines the observed state of Casdoor
type CasdoorStatus struct {
	// Casdoor status
	// +kubebuilder:default:="Pending"
	Status CasdoorStatusEnum `json:"status"`

	// reason if pending or failed
	Reason string `json:"reason,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Casdoor is the Schema for the casdoors API
type Casdoor struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CasdoorSpec   `json:"spec"`
	Status CasdoorStatus `json:"status,omitempty"`
}

// GetHttpPort Get port from `app.conf`
func (c *Casdoor) GetHttpPort() (int32, error) {
	var httpPort int32
	if portString, ok := c.Spec.AppConf["httpport"]; ok {
		if port, err := strconv.ParseInt(portString, 10, 32); err != nil {
			return 0, err
		} else {
			httpPort = int32(port)
		}
	} else {
		httpPort = 8000
	}
	return httpPort, nil
}

//+kubebuilder:object:root=true

// CasdoorList contains a list of Casdoor
type CasdoorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Casdoor `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Casdoor{}, &CasdoorList{})
}
