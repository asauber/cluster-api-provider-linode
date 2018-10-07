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

package linode

import (
	"github.com/golang/glog"

	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type LinodeClusterClient struct {
	client client.Client
}

type ClusterActuatorParams struct {
}

func NewClusterActuator(m manager.Manager, params ClusterActuatorParams) (*LinodeClusterClient, error) {
	return &LinodeClusterClient{
		client: m.GetClient(),
	}, nil
}

func (lcc *LinodeClusterClient) Reconcile(cluster *clusterv1.Cluster) error {
	glog.Infof("Reconciling cluster %v.", cluster.Name)
	return nil
}

func (lcc *LinodeClusterClient) Delete(cluster *clusterv1.Cluster) error {
	glog.Infof("Deleting cluster %v.", cluster.Name)
	return nil
}
