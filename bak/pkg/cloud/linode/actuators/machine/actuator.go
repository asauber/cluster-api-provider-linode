// Copyright © 2018 The Kubernetes Authors.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package machine

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/record"

	"github.com/displague/cluster-api-provider-linode/cloud/linode/actuators/machine/machinesetup"
	linodeconfigv1 "github.com/displague/cluster-api-provider-linode/cloud/linode/providerconfig/v1alpha1"
	"github.com/displague/cluster-api-provider-linode/pkg/ssh"
	"github.com/displague/cluster-api-provider-linode/pkg/sshutil"
	"github.com/displague/cluster-api-provider-linode/pkg/util"
	clustercommon "sigs.k8s.io/cluster-api/pkg/apis/cluster/common"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	"sigs.k8s.io/cluster-api/pkg/cert"
	client "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset/typed/cluster/v1alpha1"
	"sigs.k8s.io/cluster-api/pkg/kubeadm"

	"github.com/golang/glog"
	"github.com/linode/linodego"
)

const (
	ProviderName = "linode"

	createCheckPeriod  = 10 * time.Second
	createCheckTimeout = 5 * time.Minute

	eventReasonCreate = "Create"
	eventReasonUpdate = "Update"
	eventReasonDelete = "Delete"

	NameAnnotationKey   = "instance-name"
	IDAnnotationKey     = "instance-id"
	RegionAnnotationKey = "instance-region"
)

func init() {
	actuator, err := NewMachineActuator(ActuatorParams{})
	if err != nil {
		glog.Fatalf("Error creating cluster provisioner for %v : %v", ProviderName, err)
	}
	clustercommon.RegisterClusterProvisioner(ProviderName, actuator)
}

type LinodeClientKubeadm interface {
	TokenCreate(params kubeadm.TokenCreateParams) (string, error)
}

type LinodeClientMachineSetupConfigGetter interface {
	GetMachineSetupConfig() (machinesetup.MachineSetupConfig, error)
}

// LinodeClientSSHCreds has path to the private key and user associated with it.
type LinodeClientSSHCreds struct {
	privateKeyPath string
	publicKeyPath  string
	user           string
}

// LinodeClient is responsible for performing machine reconciliation
type LinodeClient struct {
	linodegoClient            *linodego.Client
	certificateAuthority      *cert.CertificateAuthority
	scheme                    *runtime.Scheme
	linodeProviderConfigCodec *linodeconfigv1.LinodeProviderConfigCodec
	kubeadm                   LinodeClientKubeadm
	ctx                       context.Context
	SSHCreds                  LinodeClientSSHCreds
	v1Alpha1Client            client.ClusterV1alpha1Interface
	eventRecorder             record.EventRecorder
	machineSetupConfigGetter  LinodeClientMachineSetupConfigGetter
}

// ActuatorParams holds parameter information for LinodeClient
type ActuatorParams struct {
	Kubeadm                  LinodeClientKubeadm
	CertificateAuthority     *cert.CertificateAuthority
	V1Alpha1Client           client.ClusterV1alpha1Interface
	EventRecorder            record.EventRecorder
	MachineSetupConfigGetter LinodeClientMachineSetupConfigGetter
}

// NewMachineActuator creates a new LinodeClient
func NewMachineActuator(params ActuatorParams) (*LinodeClient, error) {
	scheme, err := linodeconfigv1.NewScheme()
	if err != nil {
		return nil, err
	}

	codec, err := linodeconfigv1.NewCodec()
	if err != nil {
		return nil, err
	}

	var user, privateKeyPath, publicKeyPath string
	if _, err := os.Stat("/etc/sshkeys/private"); err == nil {
		privateKeyPath = "/etc/sshkeys/private"

		// TODO: A PR is coming for this. We will match images to OSes. This will be also needed for userdata.
		user = "root"
	}
	if _, err := os.Stat("/etc/sshkeys/public"); err == nil {
		publicKeyPath = "/etc/sshkeys/public"
	}

	return &LinodeClient{
		linodegoClient:       getLinodeClient(),
		certificateAuthority: params.CertificateAuthority,
		scheme:               scheme,
		linodeProviderConfigCodec: codec,
		kubeadm:                   getKubeadm(params),
		ctx:                       context.Background(),
		SSHCreds: LinodeClientSSHCreds{
			privateKeyPath: privateKeyPath,
			publicKeyPath:  publicKeyPath,
			user:           user,
		},
		v1Alpha1Client:           params.V1Alpha1Client,
		eventRecorder:            params.EventRecorder,
		machineSetupConfigGetter: params.MachineSetupConfigGetter,
	}, nil
}

// Create creates a machine and is invoked by the Machine Controller
func (l *LinodeClient) Create(cluster *clusterv1.Cluster, machine *clusterv1.Machine) error {
	if l.machineSetupConfigGetter == nil {
		return fmt.Errorf("machine setup config is required")
	}

	machineConfig, err := l.decodeMachineProviderConfig(machine.Spec.ProviderConfig)
	if err != nil {
		return fmt.Errorf("error decoding provided machineConfig: %v", err)
	}

	if err := l.validateMachine(machineConfig); err != nil {
		return fmt.Errorf("error validating provided machineConfig: %v", err)
	}

	instance, err := l.instanceExists(machine)
	if err != nil {
		return err
	}
	if instance != nil {
		glog.Info("Skipping the machine that already exists.")
		return nil
	}

	token, err := l.getKubeadmToken()
	if err != nil {
		return err
	}

	var parsedMetadata string
	configParams := &machinesetup.ConfigParams{
		Image:    machineConfig.Image,
		Versions: machine.Spec.Versions,
	}
	machineSetupConfig, err := l.machineSetupConfigGetter.GetMachineSetupConfig()
	if err != nil {
		return err
	}
	metadata, err := machineSetupConfig.GetUserdata(configParams)
	if err != nil {
		return err
	}
	if util.IsMachineMaster(machine) {
		parsedMetadata, err = masterUserdata(cluster, machine, l.certificateAuthority, machineConfig.Image, token, metadata)
		if err != nil {
			return err
		}
	} else {
		parsedMetadata, err = nodeUserdata(cluster, machine, machineConfig.Image, token, metadata)
		if err != nil {
			return err
		}
	}

	instanceSSHKeys := []linodego.InstanceCreateSSHKey{}
	// Add machineSpec provided keys.
	for _, k := range machineConfig.SSHPublicKeys {
		sshkey, err := sshutil.NewKeyFromString(k)
		if err != nil {
			return err
		}
		if err := sshkey.Create(l.ctx, l.linodegoClient.Keys); err != nil {
			return err
		}
		instanceSSHKeys = append(instanceSSHKeys, linodego.InstanceCreateSSHKey{Fingerprint: sshkey.FingerprintMD5})
	}
	// Add machineActuator public key.
	if l.SSHCreds.publicKeyPath != "" {
		sshkey, err := sshutil.NewKeyFromFile(l.SSHCreds.publicKeyPath)
		if err != nil {
			return err
		}
		if err := sshkey.Create(l.ctx, l.linodegoClient.Keys); err != nil {
			return err
		}
		instanceSSHKeys = append(instanceSSHKeys, linodego.InstanceCreateSSHKey{Fingerprint: sshkey.FingerprintMD5})
	}

	instanceCreateReq := &linodego.InstanceCreateRequest{
		Name:   machine.Name,
		Region: machineConfig.Region,
		Size:   machineConfig.Size,
		Image: linodego.InstanceCreateImage{
			Slug: machineConfig.Image,
		},
		Backups:           machineConfig.Backups,
		IPv6:              machineConfig.IPv6,
		PrivateNetworking: machineConfig.PrivateNetworking,
		Monitoring:        machineConfig.Monitoring,
		Tags: append([]string{
			string(machine.UID),
		}, machineConfig.Tags...),
		SSHKeys:  instanceSSHKeys,
		UserData: parsedMetadata,
	}

	instance, _, err = l.linodegoClient.CreateInstance(l.ctx, instanceCreateReq)
	if err != nil {
		return err
	}

	//We need to wait until the instance really got created as tags will be only applied when the instance is running.
	err = wait.Poll(createCheckPeriod, createCheckTimeout, func() (done bool, err error) {
		instance, _, err := l.linodegoClient.Instances.Get(l.ctx, instance.ID)
		if err != nil {
			return false, err
		}
		if sets.NewString(instance.Tags...).Has(string(machine.UID)) && len(instance.Networks.V4) > 0 {
			return true, nil
		}
		glog.Infof("waiting until machine %s gets fully created", machine.Name)
		return false, nil
	})

	if machine.ObjectMeta.Annotations == nil {
		machine.ObjectMeta.Annotations = map[string]string{}
	}
	machine.ObjectMeta.Annotations[NameAnnotationKey] = instance.Name
	machine.ObjectMeta.Annotations[IDAnnotationKey] = strconv.Itoa(instance.ID)
	machine.ObjectMeta.Annotations[RegionAnnotationKey] = instance.Region.Name

	_, err = l.v1Alpha1Client.Machines(machine.Namespace).Update(machine)
	if err != nil {
		return err
	}
	err = l.updateInstanceStatus(machine)
	if err != nil {
		return err
	}

	l.eventRecorder.Eventf(machine, corev1.EventTypeNormal, eventReasonCreate, "machine %s successfully created", machine.ObjectMeta.Name)
	return nil
}

// Delete deletes a machine and is invoked by the Machine Controller
func (l *LinodeClient) Delete(cluster *clusterv1.Cluster, machine *clusterv1.Machine) error {
	instance, err := l.instanceExists(machine)
	if err != nil {
		return err
	}
	if instance == nil {
		glog.Info("Skipping the machine that doesn't exist.")
		return nil
	}

	_, err = l.linodegoClient.DeleteInstance(l.ctx, instance.ID)
	if err != nil {
		return err
	}

	l.eventRecorder.Eventf(machine, corev1.EventTypeNormal, eventReasonDelete, "machine %s successfully deleted", machine.ObjectMeta.Name)
	return nil
}

// Update updates a machine and is invoked by the Machine Controller
func (l *LinodeClient) Update(cluster *clusterv1.Cluster, goalMachine *clusterv1.Machine) error {
	goalMachineConfig, err := l.decodeMachineProviderConfig(goalMachine.Spec.ProviderConfig)
	if err != nil {
		return fmt.Errorf("error decoding provided machineConfig: %v", err)
	}

	if err := l.validateMachine(goalMachineConfig); err != nil {
		return fmt.Errorf("error validating provided machineConfig: %v", err)
	}

	instance, err := l.instanceExists(goalMachine)
	if err != nil {
		return err
	}
	if instance == nil {
		return fmt.Errorf("machine %s doesn't exist", goalMachine.Name)
	}

	status, err := l.instanceStatus(goalMachine)
	if err != nil {
		return err
	}

	currentMachine := (*clusterv1.Machine)(status)
	if currentMachine == nil {
		return fmt.Errorf("status annotation not set")
	}

	if !l.requiresUpdate(currentMachine, goalMachine) {
		return nil
	}

	if util.IsMachineMaster(currentMachine) {
		if currentMachine.Spec.Versions.ControlPlane != goalMachine.Spec.Versions.ControlPlane {
			cmd, err := l.upgradeCommandMasterControlPlane(goalMachine)
			if err != nil {
				return err
			}

			sshClient, err := ssh.NewClient(cluster.Status.APIEndpoints[0].Host, "22", l.SSHCreds.user, l.SSHCreds.privateKeyPath)
			if err != nil {
				return err
			}
			err = sshClient.Connect()
			if err != nil {
				return err
			}

			for _, c := range cmd {
				_, err = sshClient.Execute(c)
				if err != nil {
					return err
				}
			}
			err = l.updateInstanceStatus(goalMachine)
			if err != nil {
				return err
			}
			err = sshClient.Close()
			if err != nil {
				return err
			}
			l.eventRecorder.Eventf(goalMachine, corev1.EventTypeNormal, eventReasonUpdate, "machine %s control plane successfully updated", goalMachine.Name)
		}

		if currentMachine.Spec.Versions.Kubelet != goalMachine.Spec.Versions.Kubelet {
			cmd, err := l.upgradeCommandMasterKubelet(goalMachine)
			if err != nil {
				return err
			}

			sshClient, err := ssh.NewClient(cluster.Status.APIEndpoints[0].Host, "22", l.SSHCreds.user, l.SSHCreds.privateKeyPath)
			if err != nil {
				return err
			}
			err = sshClient.Connect()
			if err != nil {
				return err
			}

			for _, c := range cmd {
				_, err = sshClient.Execute(c)
				if err != nil {
					return err
				}
			}

			err = l.updateInstanceStatus(goalMachine)
			if err != nil {
				return err
			}
			err = sshClient.Close()
			if err != nil {
				return err
			}
			l.eventRecorder.Eventf(goalMachine, corev1.EventTypeNormal, eventReasonUpdate, "machine %s kubelet successfully updated", goalMachine.Name)
		}
	} else {
		glog.Infof("re-creating node %s for update", currentMachine.Name)
		err = l.Delete(cluster, currentMachine)
		if err != nil {
			return err
		}

		goalMachine.Annotations[IDAnnotationKey] = ""
		err = l.Create(cluster, goalMachine)
		if err != nil {
			return err
		}
		l.eventRecorder.Eventf(goalMachine, corev1.EventTypeNormal, eventReasonUpdate, "node %s successfully updated", goalMachine.ObjectMeta.Name)
	}

	return nil
}

// Exists test for the existance of a machine and is invoked by the Machine Controller
func (l *LinodeClient) Exists(cluster *clusterv1.Cluster, machine *clusterv1.Machine) (bool, error) {
	instance, err := l.instanceExists(machine)
	if err != nil {
		return false, err
	}
	if instance != nil {
		return true, nil
	}
	return false, nil
}

func getKubeadm(params ActuatorParams) LinodeClientKubeadm {
	if params.Kubeadm == nil {
		return kubeadm.New()
	}
	return params.Kubeadm
}

func (l *LinodeClient) getKubeadmToken() (string, error) {
	tokenParams := kubeadm.TokenCreateParams{
		Ttl: time.Duration(30) * time.Minute,
	}

	token, err := l.kubeadm.TokenCreate(tokenParams)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(token), nil
}

// instanceExists returns instance with provided name if it already exists in the cloud.
func (l *LinodeClient) instanceExists(machine *clusterv1.Machine) (*linodego.Instance, error) {
	if strID, ok := machine.ObjectMeta.Annotations[IDAnnotationKey]; ok {
		if strID == "" {
			return nil, nil
		}
		id, err := strconv.Atoi(strID)
		if err != nil {
			return nil, err
		}
		instance, resp, err := l.linodegoClient.Instances.Get(l.ctx, id)
		if err != nil {
			if resp != nil && resp.StatusCode == http.StatusNotFound {
				// Machine exists as an object, but Instance is already deleted.
				return nil, nil
			}
			return nil, err
		}
		if instance != nil {
			return instance, nil
		}
		// Fallback to searching by name.
	}

	instances, _, err := l.linodegoClient.ListInstances(l.ctx, &linodego.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, d := range instances {
		if d.Name == machine.Name && sets.NewString(d.Tags...).Has(string(machine.UID)) {
			glog.Infof("found a machine %s by name", d.Name)
			return &d, nil
		}
	}
	return nil, nil
}

// requiresUpdate compares ObjectMeta, ProviderConfig and Versions object of two machines.
func (l *LinodeClient) requiresUpdate(a *clusterv1.Machine, b *clusterv1.Machine) bool {
	// Do not want status changes. Do want changes that impact machine provisioning
	return !equality.Semantic.DeepEqual(a.Spec.ObjectMeta, b.Spec.ObjectMeta) ||
		!equality.Semantic.DeepEqual(a.Spec.ProviderConfig, b.Spec.ProviderConfig) ||
		!equality.Semantic.DeepEqual(a.Spec.Versions, b.Spec.Versions)
}

func (l *LinodeClient) validateMachine(providerConfig *linodeconfigv1.LinodeMachineProviderConfig) error {
	if len(providerConfig.Image) == 0 {
		return fmt.Errorf("image slug must be provided")
	}
	if len(providerConfig.Region) == 0 {
		return fmt.Errorf("region must be provided")
	}
	if len(providerConfig.Size) == 0 {
		return fmt.Errorf("size must be provided")
	}

	return nil
}

// decodeMachineProviderConfig returns Linode MachineProviderConfig from upstream Spec.
func (l *LinodeClient) decodeMachineProviderConfig(providerConfig clusterv1.ProviderConfig) (*linodeconfigv1.LinodeMachineProviderConfig, error) {
	var config linodeconfigv1.LinodeMachineProviderConfig
	err := l.linodeProviderConfigCodec.DecodeFromProviderConfig(providerConfig, &config)
	if err != nil {
		return nil, err
	}

	return &config, err
}
