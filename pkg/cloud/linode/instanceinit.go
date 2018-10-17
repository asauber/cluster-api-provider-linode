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
	"bytes"
	"fmt"
	"text/template"
	"time"

	linodeconfigv1 "github.com/displague/cluster-api-provider-linode/pkg/apis/linodeproviderconfig/v1alpha1"
	"github.com/golang/glog"
	"golang.org/x/net/context"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
)

func isMaster(roles []linodeconfigv1.MachineRole) bool {
	glog.Infof("roles %v", roles)
	for _, r := range roles {
		if r == linodeconfigv1.MasterRole {
			return true
		}
	}
	return false
}

type initScriptParams struct {
	Token        string
	Cluster      *clusterv1.Cluster
	Machine      *clusterv1.Machine
	MachineLabel string

	// These fields are optional
	PodCIDR        string
	ServiceCIDR    string
	MasterEndpoint string
}

/*
 * TODO: Render different shell scripts for each combination of
 *	{ Machine State, Operating System Image, Roles }
 * Other cluster-api implmentations do this partially with ConfigMaps and logic
 * that tries to match Machine fields with scripts in those ConfigMaps.
 * However, the way in which they are implemented is currently too rigid, as
 * those scripts are not composable or broken out for different machine states
 * to be used with RequeueAfterError.
 */

func (lc *LinodeClient) getInitScript(token string, cluster *clusterv1.Cluster, machine *clusterv1.Machine, config *linodeconfigv1.LinodeMachineProviderConfig) (string, error) {
	if isMaster(config.Roles) {
		params := initScriptParams{
			Token:        token,
			Cluster:      cluster,
			Machine:      machine,
			MachineLabel: lc.MachineLabel(cluster, machine),
			PodCIDR:      cluster.Spec.ClusterNetwork.Pods.CIDRBlocks[0],
			ServiceCIDR:  cluster.Spec.ClusterNetwork.Services.CIDRBlocks[0],
		}

		var buf bytes.Buffer
		if err := masterInitScriptTempl.Execute(&buf, params); err != nil {
			return "", err
		}
		return buf.String(), nil
	} else {
		latestCluster, err := lc.waitForClusterEndpoint(cluster)
		if err != nil {
			return "", err
		}

		params := initScriptParams{
			Token:          token,
			Cluster:        latestCluster,
			Machine:        machine,
			MachineLabel:   lc.MachineLabel(latestCluster, machine),
			MasterEndpoint: endpoint(latestCluster.Status.APIEndpoints[0]),
		}

		var buf bytes.Buffer
		if err := nodeInitScriptTempl.Execute(&buf, params); err != nil {
			return "", err
		}
		return buf.String(), nil
	}
}

func (lc *LinodeClient) waitForClusterEndpoint(cluster *clusterv1.Cluster) (*clusterv1.Cluster, error) {
	pollCluster := cluster.DeepCopy()

	err := wait.Poll(10*time.Second, 10*time.Minute, func() (done bool, err error) {
		err = lc.client.Get(context.Background(),
			types.NamespacedName{Namespace: cluster.GetNamespace(), Name: cluster.GetName()},
			pollCluster)
		if err != nil {
			return false, err
		}
		if len(pollCluster.Status.APIEndpoints) > 0 {
			glog.Infof("Cluster has an endpoint %v", pollCluster.Status.APIEndpoints[0])
			return true, nil
		}
		glog.Infof("waiting until cluster has an endpoint %s", cluster.Name)
		return false, nil
	})

	return pollCluster, err
}

func endpoint(apiEndpoint clusterv1.APIEndpoint) string {
	return fmt.Sprintf("%s:%d", apiEndpoint.Host, apiEndpoint.Port)
}

var (
	masterInitScriptTempl *template.Template
	nodeInitScriptTempl   *template.Template
)

func init() {
	masterInitScriptTempl = template.Must(template.New("masterInitScript").Parse(masterInitScript))
	nodeInitScriptTempl = template.Must(template.New("nodeInitScript").Parse(nodeInitScript))
}

/*
 * TODO: Factor out the common parts of these scripts, break them into
 * components that can be used with RequeueAfterError
 */
const masterInitScript = `#!/bin/bash
TOKEN={{ .Token }}
K8SVERSION={{ .Machine.Spec.Versions.Kubelet }}
HOSTNAME={{ .MachineLabel }}
NAMESPACE={{ .Machine.ObjectMeta.Namespace }}
MACHINE=$NAMESPACE
MACHINE+="/"
MACHINE+={{ .Machine.ObjectMeta.Name }}
CONTROL_PLANE_VERSION={{ .Machine.Spec.Versions.ControlPlane }}
SERVICE_DOMAIN={{ .Cluster.Spec.ClusterNetwork.ServiceDomain }}
POD_CIDR={{ .PodCIDR }}
SERVICE_CIDR={{ .ServiceCIDR }}

echo "masterscript" > /var/log/test.txt

set -e
set -x
(
ARCH=amd64

# Set hostname
hostnamectl set-hostname "$HOSTNAME" && hostname -F /etc/hostname

# Turn off swap
head -n -1 /etc/fstab > tempfstab ; mv tempfstab /etc/fstab
swapoff -a

# Install pre-k
# TODO: Make pre-k version dynamic
curl -fsSL --retry 5 -o pre-k https://cdn.appscode.com/binaries/pre-k/1.11.0/pre-k-linux-amd64 \
	&& chmod +x pre-k \
	&& mv pre-k /usr/bin/

# Install Docker
# TODO: Install specific version of Docker based on Machine config
apt-get update
apt-get install -y docker.io apt-transport-https
systemctl enable docker

# Install Kubernetes binaries
curl -s https://packages.cloud.google.com/apt/doc/apt-key.gpg | apt-key add -
echo "deb http://apt.kubernetes.io/ kubernetes-xenial main" | tee /etc/apt/sources.list.d/kubernetes.list        
apt-get update -qq

# Get Debian package version with Kubernetes version prefix
function getversion() {
	name=$1
	prefix=$2
	version=$(apt-cache madison $name | awk '{ print $3 }' | grep ^$prefix | head -n1)
	if [[ -z "$version" ]]; then
		echo "Can\'t find package $name with prefix $prefix"
		exit 1
	fi
	echo $version
}

KUBEADMVERSION=$(getversion kubeadm ${K8SVERSION}-)
KUBELETVERSION=$(getversion kubelet ${K8SVERSION}-)
KUBECTLVERSION=$(getversion kubectl ${K8SVERSION}-)

apt-get install -qy kubeadm=${KUBEADMVERSION} kubelet=${KUBELETVERSION} kubectl=${KUBECTLVERSION}
apt-mark hold kubeadm kubelet kubectl

# TODO: Disable password login

PUBLICIP=$(pre-k machine public-ips --all=false 2>/dev/null)
PRIVATEIP=$(pre-k machine private-ips --all=false 2>/dev/null)

# Set up kubeadm config file to pass parameters to kubeadm init.
touch /etc/kubernetes/kubeadm_config.yaml
cat > /etc/kubernetes/kubeadm_config.yaml <<EOF
apiVersion: kubeadm.k8s.io/v1alpha1
kind: MasterConfiguration
kubernetesVersion: v${K8SVERSION}
token: ${TOKEN}
networking:
  serviceSubnet: ${SERVICE_CIDR}
  podSubnet: ${POD_CIDR}
  dnsDomain: ${SERVICE_DOMAIN}
controllerManagerExtraArgs:
  cluster-cidr: ${POD_CIDR}
  service-cluster-ip-range: ${SERVICE_CIDR}
apiServerCertSANs:
- ${PUBLICIP}
- ${PRIVATEIP}
- ${HOSTNAME}
- 127.0.0.1
EOF

# TODO: Generate kubelet configuration for custom service domain

function install_custom_ca () {
	if [ ! -n "$MASTER_CA_CERTIFICATE" ]; then
		return
	fi
	if [ ! -n "$MASTER_CA_PRIVATE_KEY" ]; then
		return
	fi

	echo "Installing custom certificate authority..."

	PKI_PATH=/etc/kubernetes/pki
	mkdir -p ${PKI_PATH}
	CA_CERT_PATH=${PKI_PATH}/ca.crt
	echo ${MASTER_CA_CERTIFICATE} | base64 -d > ${CA_CERT_PATH}
	chmod 0644 ${CA_CERT_PATH}
	CA_KEY_PATH=${PKI_PATH}/ca.key
	echo ${MASTER_CA_PRIVATE_KEY} | base64 -d > ${CA_KEY_PATH}
	chmod 0600 ${CA_KEY_PATH}
}
install_custom_ca

kubeadm init --config /etc/kubernetes/kubeadm_config.yaml

mkdir -p $HOME/.kube && cp -i /etc/kubernetes/admin.conf $HOME/.kube/config

# Annotate node.
for tries in $(seq 1 60); do
	kubectl --kubeconfig /etc/kubernetes/kubelet.conf annotate --overwrite node ${HOSTNAME} machine=${MACHINE} && break
	sleep 1
done 

# Install Calico CNI
function install_cni() {
	set -e

	wget https://docs.projectcalico.org/v3.2/getting-started/kubernetes/installation/hosted/rbac-kdd.yaml
	wget https://docs.projectcalico.org/v3.2/getting-started/kubernetes/installation/hosted/kubernetes-datastore/calico-networking/1.7/calico.yaml

	sed -i "s|192.168.0.0/16|$POD_CIDR|" calico.yaml

	kubectl apply --kubeconfig /etc/kubernetes/admin.conf -f rbac-kdd.yaml
	kubectl apply --kubeconfig /etc/kubernetes/admin.conf -f calico.yaml 
}
install_cni

echo done
) 2>&1 | tee /var/log/startup.log
`

const nodeInitScript = `#!/bin/bash
TOKEN={{ .Token }}
K8SVERSION={{ .Machine.Spec.Versions.Kubelet }}
HOSTNAME={{ .MachineLabel }}
NAMESPACE={{ .Machine.ObjectMeta.Namespace }}
MACHINE=$NAMESPACE
MACHINE+="/"
MACHINE+={{ .Machine.ObjectMeta.Name }}
SERVICE_DOMAIN={{ .Cluster.Spec.ClusterNetwork.ServiceDomain }}
ENDPOINT={{ .MasterEndpoint }}

echo "masterscript" > /var/log/test.txt

# Set hostname
echo "0" >> /var/log/test.txt
set -e
set -x
(
echo "1" >> /var/log/test.txt
ARCH=amd64

echo "2" >> /var/log/test.txt

hostnamectl set-hostname "$HOSTNAME" && hostname -F /etc/hostname

# Turn off swap
head -n -1 /etc/fstab > tempfstab ; mv tempfstab /etc/fstab
swapoff -a

# Install Docker
# TODO: Install specific version of Docker based on Machine config
apt-get update
apt-get install -y docker.io apt-transport-https
systemctl enable docker


# Install Kubernetes binaries
curl -s https://packages.cloud.google.com/apt/doc/apt-key.gpg | apt-key add -
echo "deb http://apt.kubernetes.io/ kubernetes-xenial main" | tee /etc/apt/sources.list.d/kubernetes.list        
apt-get update -qq

# Get Debian package version with Kubernetes version prefix
function getversion() {
	name=$1
	prefix=$2
	version=$(apt-cache madison $name | awk '{ print $3 }' | grep ^$prefix | head -n1)
	if [[ -z "$version" ]]; then
		echo "Can\'t find package $name with prefix $prefix"
		exit 1
	fi
	echo $version
}

KUBEADMVERSION=$(getversion kubeadm ${K8SVERSION}-)
KUBELETVERSION=$(getversion kubelet ${K8SVERSION}-)
KUBECTLVERSION=$(getversion kubectl ${K8SVERSION}-)

apt-get install -qy kubeadm=${KUBEADMVERSION} kubelet=${KUBELETVERSION} kubectl=${KUBECTLVERSION}
apt-mark hold kubeadm kubelet kubectl

# TODO: Disable password login

# TODO: Modify kubelet configuration for custom service domain

kubeadm join --token "${TOKEN}" "${ENDPOINT}" --ignore-preflight-errors=all --discovery-token-unsafe-skip-ca-verification

# Annotate node.
for tries in $(seq 1 60); do
	kubectl --kubeconfig /etc/kubernetes/kubelet.conf annotate --overwrite node ${HOSTNAME} machine=${MACHINE} && break
	sleep 1
done 

echo done
) 2>&1 | tee /var/log/startup.log
`
