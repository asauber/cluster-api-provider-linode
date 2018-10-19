/*
Copyright 2018 Linode, LLC.
Copyright 2018 The Kubernetes Authors.

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

package v1alpha1

import (
	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// LinodeClusterProviderConfig is the Schema for the linodeclusterproviderconfigs API
// +k8s:openapi-gen=true
type LinodeClusterProviderConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	AuthorizedKeys []string `json:"authorizedKeys,omitempty"`
}

func (c *LinodeClusterProviderConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type temp LinodeClusterProviderConfig

	t := (*temp)(c)

	glog.Info("in UnmarshalYAML")

	var m map[string]interface{}

	errUnmarshal := unmarshal(&m)

	if errUnmarshal != nil {
		return errUnmarshal
	}

	glog.Infof("in UnmarshalYAML: raw result is %#v", m)

	errUnmarshal = unmarshal(t)

	if errUnmarshal != nil {
		return errUnmarshal
	}

	glog.Infof("in UnmarshalYAML: meta value is %#v", t.TypeMeta)

	return nil
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// LinodeClusterProviderConfigList contains a list of LinodeClusterProviderConfig
type LinodeClusterProviderConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LinodeClusterProviderConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LinodeClusterProviderConfig{}, &LinodeClusterProviderConfigList{})
}
