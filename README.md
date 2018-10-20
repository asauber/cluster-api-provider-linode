# Kubernetes cluster-api-provider-linode 

This repository hosts a concrete implementation of a provider for
[Linode](https://www.linode.com/) for the [cluster-api
project](https://github.com/kubernetes-sigs/cluster-api).

## Project Status

This project is currently Work-in-Progress and may not be production ready.
There is no backwards-compatibility guarantee at this point. Checkout the
Features portion of the README for details about the project status.

## Getting Started

### Prerequisites

In order to create a cluster using this cluster-api implementation, you need
the following tools installed on your local machine. These are installation
insturctions for macOS. For installation instructions on other platforms,
visit the respective links.

* [Go toolchain](https://golang.org/doc/install)

```bash
brew install go
```

* This Cluster-API implementation

```bash
go get github.com/asauber/cluster-api-provider-linode
```

* [`kubectl`](https://kubernetes.io/docs/tasks/tools/install-kubectl/)

```bash
brew install kubernetes-cli
```

* [`minikube`](https://kubernetes.io/docs/tasks/tools/install-minikube/) version 0.28.1

```bash
curl -Lo minikube https://storage.googleapis.com/minikube/releases/v0.28.1/minikube-darwin-amd64 && chmod +x minikube && sudo cp minikube /usr/local/bin/ && rm minikube
```

* `virtualbox` on macOS or [`kvm2`](https://github.com/kubernetes/minikube/blob/master/docs/drivers.md) on Linux.

```bash
brew cask install virutalbox
```

* [Linode API Access Token generated](https://cloud.linode.com/profile/tokens) and set as the `LINODE_TOKEN` environment variable

```bash
export LINODE_TOKEN="<Your Linode API Token>"
echo "export LINODE_TOKEN=<Your Linode API Token>" >> ~/.bash_profile
```

* [The Linode CLI](https://www.linode.com/docs/platform/api/using-the-linode-cli/)

```
pip install linode-cli --upgrade
linode-cli
```

* [`kustomize`](https://github.com/kubernetes-sigs/kustomize/blob/master/docs/INSTALL.md)

```bash
brew install kustomize
```

## Creating a Cluster

To create a cluster using `cluster-api-provider-linode`, you

1. Start minikube
1. Deploy a collection of Kubernetes resources to minikube that implement
the cluster-api, the "provider components"
1. Deploy a collection of Kubernetes resources to minikube that represent
your new cluster

First, start minikube

```
minikube start
```

Next, use `kustomize` to render the templates for the provider components.

```bash
kustomize build config/default/ > cmd/examples/linode/provider-components.yaml
echo "---" >> cmd/examples/linode/provider-components.yaml
kustomize build vendor/sigs.k8s.io/cluster-api/config/default/ >> cmd/examples/linode/provider-components.yaml
```

This generates a YAML file which will be used to create the namespaces,
custom resource defintions, roles, rolebindings, and services which run
the Linode cluster-api controllers and the upstream cluster-api controllers.

Deploy these resources to minikube.

```bash
kubectl create -f cmd/examples/linode/provider-components.yaml
```

Next, generate manifests for your cluster. You must provide a path to a
public key that will be used to access your cluster.

```bash
cd cmd/examples/linode
./generate-yaml.sh $HOME/.ssh/id_rsa.pub
cd ../../..
```

This generates a YAML file which will be used to create a namespace, cluster,
machine, and secrets for the new cluster. Feel free to edit this manifest,
for example, to add some additional machines with the "Node" Role. If you
would like to use the manifest to add an additional cluster, you will need to
edit the namespace used by the all of the resources. Note that only one
Master machine and one Cluster resource per namespace is currently supported.

Create these cluster resources

```bash
kubectl create -f cmd/examples/linode/cluster.yaml
```

You can observe the logs of the controllers while the cluster is being
created

```bash
kubectl logs -f cluster-api-provider-linode-controller-manager-0 -n cluster-api-provider-linode-system
```

### Interacting With Your New Cluster

Download the admin kubeconfig for your cluster. The first argument should be
namespace used for your cluster resources.

```bash
hack/get_kubeconfig.sh cluster0
```

You can now interact with the cluster via kubectl. Note that it may take a
while before all services are running on the cluster.

```bash
kubectl --kubeconfig kubeconfig get nodes
kubectl --kubeconfig kubeconfig get all --all-namespaces
```

The cluster is fully functional when at least the following services are runnning

```
NAMESPACE     NAME                                                   READY     STATUS    RESTARTS   AGE
kube-system   calico-node-gb75n                                      2/2       Running   0          15m
kube-system   calico-node-mjtqv                                      2/2       Running   0          15m
kube-system   coredns-78fcdf6894-6tbxc                               1/1       Running   0          15m
kube-system   coredns-78fcdf6894-6zgpp                               1/1       Running   0          15m
kube-system   etcd-cluster01-kbkwk-master-9ggst                      1/1       Running   0          15m
kube-system   kube-apiserver-cluster01-kbkwk-master-9ggst            1/1       Running   0          15m
kube-system   kube-controller-manager-cluster01-kbkwk-master-9ggst   1/1       Running   0          15m
kube-system   kube-proxy-g8fc6                                       1/1       Running   0          15m
kube-system   kube-proxy-sc5km                                       1/1       Running   0          15m
kube-system   kube-scheduler-cluster01-kbkwk-master-9ggst            1/1       Running   0          15m
```