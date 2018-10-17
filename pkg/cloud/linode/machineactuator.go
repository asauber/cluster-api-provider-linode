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
	"net/http"

	linodeconfigv1 "github.com/displague/cluster-api-provider-linode/pkg/apis/linodeproviderconfig/v1alpha1"
	"github.com/golang/glog"
	"github.com/linode/linodego"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	yaml "gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	bootstraputil "k8s.io/client-go/tools/bootstrap/token/util"
	"k8s.io/client-go/tools/record"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	apierrors "sigs.k8s.io/cluster-api/pkg/errors"
	"sigs.k8s.io/cluster-api/pkg/kubeadm"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	ProviderName = "linode"
)

const (
	createEventAction        = "Create"
	deleteEventAction        = "Delete"
	noEventAction            = ""
	linodeAPITokenSecretName = "linode-api-token"
)

type LinodeClient struct {
	client        client.Client
	scheme        *runtime.Scheme
	eventRecorder record.EventRecorder
	kubeadm       *kubeadm.Kubeadm
}

type MachineActuatorParams struct {
	Scheme        *runtime.Scheme
	EventRecorder record.EventRecorder
}

func NewMachineActuator(m manager.Manager, params MachineActuatorParams) (*LinodeClient, error) {
	return &LinodeClient{
		client:        m.GetClient(),
		scheme:        params.Scheme,
		eventRecorder: params.EventRecorder,
		kubeadm:       kubeadm.New(),
	}, nil
}

func getLinodeAPIClient(client client.Client, cluster *clusterv1.Cluster) (*linodego.Client, error) {
	/*
	 * We construct a new client every time that we make a Linode API call so that
	 * the API Token Secret can be rotated at any time. We need a Cluster object
	 * so that we can associate a different API token with each Cluster.
	 */
	apiTokenSecret := &corev1.Secret{}
	namespace := cluster.GetNamespace()
	err := client.Get(context.Background(),
		types.NamespacedName{Namespace: namespace, Name: linodeAPITokenSecretName},
		apiTokenSecret)

	if err != nil {
		return nil, fmt.Errorf("error retrieving Linode API token secret for cluster %v", err)
	}

	apiKey, ok := apiTokenSecret.Data["token"]
	if !ok {
		return nil, fmt.Errorf("Linode API token secret for namespace %s is missing 'token' data", namespace)
	}

	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: string(apiKey)})
	oauth2Client := &http.Client{
		Transport: &oauth2.Transport{
			Source: tokenSource,
		},
	}
	linodeClient := linodego.NewClient(oauth2Client)
	return &linodeClient, nil
}

func (lc *LinodeClient) validateMachine(machine *clusterv1.Machine, config *linodeconfigv1.LinodeMachineProviderConfig) *apierrors.MachineError {
	if machine.Spec.Versions.Kubelet == "" {
		return apierrors.InvalidMachineConfiguration("spec.versions.kubelet can't be empty")
	}
	return nil
}

func (lc *LinodeClient) Create(cluster *clusterv1.Cluster, machine *clusterv1.Machine) error {
	glog.Infof("Creating machine %v/%v", cluster.Name, machine.Name)
	machineConfig, err := machineProviderConfig(machine.Spec.ProviderConfig)
	if err != nil {
		return lc.handleMachineError(machine, apierrors.InvalidMachineConfiguration(
			"Cannot unmarshal machine's providerConfig field: %v", err), createEventAction)
	}

	if verr := lc.validateMachine(machine, machineConfig); verr != nil {
		return lc.handleMachineError(machine, verr, createEventAction)
	}

	instance, err := lc.instanceIfExists(cluster, machine)
	if err != nil {
		return err
	}

	if instance == nil {
		label := lc.MachineLabel(cluster, machine)

		/* Create or update StackScript for machine init */
		token, err := getOrCreateJoinToken(lc.client, cluster)
		if err != nil {
			return err
		}

		script, err := lc.getInitScript(token, cluster, machine, machineConfig)
		if err != nil {
			return err
		}

		/* TODO: Very High Priority! Do not write out a StackScript per machine. Use UDFs instead */
		createOpts := linodego.StackscriptCreateOptions{
			Label:    label,
			Script:   script,
			IsPublic: false,
		}

		createOpts.Images = append(createOpts.Images, machineConfig.Image)

		linodeClient, err := getLinodeAPIClient(lc.client, cluster)
		if err != nil {
			return fmt.Errorf("Error initializing Linode API client: %v", err)
		}

		stackscript, err := linodeClient.CreateStackscript(context.Background(), createOpts)
		if err != nil {
			return fmt.Errorf("Error creating a Linode Stackscript: %v", err)
		}

		/*
		 * Use a bootstrap token as a random root password. Replace this if upstream
		 * changes. Don't store this - the idea is that no one ever knows the root password.
		 */
		rootPass, err := bootstraputil.GenerateBootstrapToken()
		if err != nil {
			return fmt.Errorf("Couldn't generate random root password: %v", err)
		}

		/* TODO: Use the Cluster spec for reading a list of public AuthorizedKeys */

		instance, err := linodeClient.CreateInstance(context.Background(), linodego.InstanceCreateOptions{
			Region:         machineConfig.Region,
			Type:           machineConfig.Type,
			Label:          lc.MachineLabel(cluster, machine),
			Image:          machineConfig.Image,
			RootPass:       rootPass,
			PrivateIP:      true,
			StackScriptID:  stackscript.ID,
			AuthorizedKeys: []string{"ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQCagK9ZjexjQrxmCvQpPm4Da7qM9tQ/ldqAHqbORTqkZbAMMm8ASkBYFP8de4+y+K/BxV2iNDo/A/0Jkaw7uJSrH645vWzCbeX2S+hQMaQp2C7HE4aua8pwjL5d1q/YnU/tiznq2Lf74BTp4/mrl4pcmOTZdlUOa/tTN0ZZlZas0+KW9dr9cn4X78HT6n7vN0TOuQQMWTsw1aFxgdNMUDf6as7Z+RzILdG5J7G7QjFBbRzcj/yaRZGpmpaPvP+KV9J+8KsnjvoMNJuvBYQapWqZqv1yUqN45J2UQ9vvJ7H/p2u8+lYvGZ0wVbRB7PTHnsR8bOSW1f0BPoMDWkW+9ZCN user@host"},
		})
		instanceCreationTimeoutSeconds := 600
		if err == nil {
			instance, err = linodeClient.WaitForInstanceStatus(
				context.Background(), instance.ID, linodego.InstanceRunning, instanceCreationTimeoutSeconds)
		}

		if err != nil {
			return lc.handleMachineError(machine, apierrors.CreateMachine(
				"error creating Linode instance: %v", err), createEventAction)
		}

		if isMaster(machineConfig.Roles) {
			lc.updateClusterEndpoint(cluster, instance)
		}

		lc.eventRecorder.Eventf(machine, corev1.EventTypeNormal, "Created", "Created Machine %v", machine.Name)
		/* TODO: Annotate Machine object with Linode ID */
	} else {
		glog.Infof("Skipped creating a VM that already exists.\n")
	}
	return nil
}

func (lc *LinodeClient) updateClusterEndpoint(cluster *clusterv1.Cluster, instance *linodego.Instance) error {
	glog.Infof("Updating cluster endpoint %v.\n", instance.IPv4[0].String())
	cluster.Status.APIEndpoints = []clusterv1.APIEndpoint{{
		Host: instance.IPv4[0].String(),
		Port: 6443,
	}}
	err := lc.client.Update(context.Background(), cluster)
	return err
}

func (lc *LinodeClient) handleMachineError(machine *clusterv1.Machine, err *apierrors.MachineError, eventAction string) error {
	/* TODO: implement machine.Status update on error */
	/*
		if lc.client != nil {
			reason := err.Reason
			message := err.Message
			machine.Status.ErrorReason = &reason
			machine.Status.ErrorMessage = &message
		}
	*/

	if eventAction != noEventAction {
		lc.eventRecorder.Eventf(machine, corev1.EventTypeWarning, "Failed"+eventAction, "%v", err.Reason)
	}

	glog.Errorf("Machine error: %v", err.Message)
	return err
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

func clusterProviderConfig(providerConfig clusterv1.ProviderConfig) (*linodeconfigv1.LinodeClusterProviderConfig, error) {
	var config linodeconfigv1.LinodeClusterProviderConfig
	if err := yaml.Unmarshal(providerConfig.Value.Raw, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

func (lc *LinodeClient) MachineLabel(cluster *clusterv1.Cluster, machine *clusterv1.Machine) string {
	return fmt.Sprintf("%s-%s", cluster.ObjectMeta.Name, machine.ObjectMeta.Name)
}

func machineProviderConfig(providerConfig clusterv1.ProviderConfig) (*linodeconfigv1.LinodeMachineProviderConfig, error) {
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

	// Get the VM via Linode label: <cluster-name>-<machine-name>
	label := lc.MachineLabel(cluster, identifyingMachine)
	linodeClient, err := getLinodeAPIClient(lc.client, cluster)
	if err != nil {
		return nil, fmt.Errorf("Error initializing Linode API client: %v", err)
	}
	instance, err := getInstanceByLabel(linodeClient, label)
	if err != nil {
		return nil, err
	}

	return instance, nil
}

func getInstanceByLabel(linodeClient *linodego.Client, label string) (*linodego.Instance, error) {
	filter := fmt.Sprintf("{ \"label\": \"%s\" }", label)
	instances, err := linodeClient.ListInstances(context.Background(), &linodego.ListOptions{
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
