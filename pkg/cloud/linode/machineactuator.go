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
	"fmt"
	"log"
	"net/http"
	"os"

	linodeconfigv1 "github.com/displague/cluster-api-provider-linode/pkg/apis/linodeproviderconfig/v1alpha1"
	"github.com/golang/glog"
	"github.com/linode/linodego"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	yaml "gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	ProviderName = "linode"
)

type LinodeClient struct {
	client       client.Client
	linodeClient *linodego.Client
	scheme       *runtime.Scheme
}

type MachineActuatorParams struct {
	Scheme *runtime.Scheme
}

func NewMachineActuator(m manager.Manager, params MachineActuatorParams) (*LinodeClient, error) {
	return &LinodeClient{
		client:       m.GetClient(),
		linodeClient: newLinodeAPIClient(),
		scheme:       params.Scheme,
	}, nil
}

func newLinodeAPIClient() *linodego.Client {
	apiKey, ok := os.LookupEnv("LINODE_API_TOKEN")
	/*
	 * TODO: Make Linode API dynamic per cluster, by associating a secret name with
	 * the Cluster Object, then constructing a new Linode client during each Resource
	 * lifecycle hook. (using the API token from that Cluster's secret)
	 */
	if !ok {
		log.Fatal("Could not find LINODE_API_TOKEN")
	}
	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: apiKey})

	oauth2Client := &http.Client{
		Transport: &oauth2.Transport{
			Source: tokenSource,
		},
	}

	linodeClient := linodego.NewClient(oauth2Client)
	linodeClient.SetDebug(true)
	return &linodeClient
}

func (lc *LinodeClient) Create(cluster *clusterv1.Cluster, machine *clusterv1.Machine) error {
	glog.Infof("TODO (Not Implemented): Creating machine with cluster %v.", cluster.Name)
	glog.Infof("TODO (Not Implemented): Creating machine %v.", machine.Name)
	return nil
}

func (lc *LinodeClient) Delete(cluster *clusterv1.Cluster, machine *clusterv1.Machine) error {
	glog.Infof("TODO (Not Implemented): Deleting machine with cluster %v.", cluster.Name)
	glog.Infof("TODO (Not Implemented): Deleting machine %v.", machine.Name)
	return nil
}

func (lc *LinodeClient) Update(cluster *clusterv1.Cluster, goalMachine *clusterv1.Machine) error {
	glog.Infof("TODO (Not Implemented): Updating machine with cluster %v.", cluster.Name)
	glog.Infof("TODO (Not Implemented): Updating machine %v.", goalMachine.Name)
	return nil
}

func (lc *LinodeClient) Exists(cluster *clusterv1.Cluster, machine *clusterv1.Machine) (bool, error) {
	glog.Infof("Checking Exists for machine %v/%v", cluster.Name, machine.Name)
	instance, err := lc.instanceIfExists(cluster, machine)
	if err != nil {
		return false, err
	}
	return (instance != nil), err
}

func clusterProviderFromProviderConfig(providerConfig clusterv1.ProviderConfig) (*linodeconfigv1.LinodeClusterProviderConfig, error) {
	var config linodeconfigv1.LinodeClusterProviderConfig
	if err := yaml.Unmarshal(providerConfig.Value.Raw, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

func machineProviderFromProviderConfig(providerConfig clusterv1.ProviderConfig) (*linodeconfigv1.LinodeMachineProviderConfig, error) {
	var config linodeconfigv1.LinodeMachineProviderConfig
	if err := yaml.Unmarshal(providerConfig.Value.Raw, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

// Gets the instance represented by the given machine
func (lc *LinodeClient) instanceIfExists(cluster *clusterv1.Cluster, machine *clusterv1.Machine) (*linodego.Instance, error) {
	identifyingMachine := machine

	// Try to use the last saved status locating the machine
	// in case instance details like the proj or zone has changed
	status, err := lc.instanceStatus(machine)
	if err != nil {
		return nil, err
	}

	if status != nil {
		identifyingMachine = (*clusterv1.Machine)(status)
	}

	// Get the VM via label: <cluster-name>-<machine-name>
	label := fmt.Sprintf("%s-%s", cluster.ObjectMeta.Name, identifyingMachine.ObjectMeta.Name)
	instance, err := lc.getInstanceByLabel(label)
	if err != nil {
		return nil, err
	}

	return instance, nil
}

func (lc *LinodeClient) getInstanceByLabel(label string) (*linodego.Instance, error) {
	filter := fmt.Sprintf("{ \"label\": \"%s\" }", label)
	instances, err := lc.linodeClient.ListInstances(context.Background(), &linodego.ListOptions{
		Filter: filter,
	})
	if err != nil {
		return nil, err
	}
	if len(instances) < 1 {
		return nil, nil
	}
	return &instances[0], nil
}

func (lc *LinodeClient) GetIP(cluster *clusterv1.Cluster, machine *clusterv1.Machine) (string, error) {
	glog.Infof("TODO (Not Implemented): Getting IP for machine with cluster %v.", cluster.Name)
	glog.Infof("TODO (Not Implemented): Getting IP for machine %v.", machine.Name)
	return "", nil
}

func (lc *LinodeClient) GetKubeConfig(cluster *clusterv1.Cluster, master *clusterv1.Machine) (string, error) {
	glog.Infof("TODO (Not Implemented): Getting KubeConfig for master with cluster %v.", cluster.Name)
	glog.Infof("TODO (Not Implemented): Getting KubeConfig for master %v.", master.Name)
	return "", nil
}
