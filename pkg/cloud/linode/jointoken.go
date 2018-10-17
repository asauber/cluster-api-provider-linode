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

	"github.com/golang/glog"
	"golang.org/x/net/context"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	bootstraputil "k8s.io/client-go/tools/bootstrap/token/util"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	joinTokenSecretName = "kubeadm-join-token"
)

func getOrCreateJoinToken(client client.Client, cluster *clusterv1.Cluster) (string, error) {
	// Look for a join token secret in the namespace of the Cluster object.
	joinTokenSecret := &k8sv1.Secret{}
	err := client.Get(context.Background(),
		types.NamespacedName{Namespace: cluster.GetNamespace(), Name: joinTokenSecretName},
		joinTokenSecret)

	if errors.IsNotFound(err) {
		// If one isn't found, create one.
		/*
		 * TODO: Regenerate token when it expires using the --kubeconfig flag of
		 * kubeadm token create. For now, we generate and use a static token per new
		 * cluster which expires after 24 hours, thus not allowing new Nodes to join
		 * the cluster after 24 hours.
		 */
		joinToken, err := bootstraputil.GenerateBootstrapToken()
		if err != nil {
			glog.Errorf("Unable to create kubeadm join token: %v", err)
			return "", err
		}
		joinTokenSecret.ObjectMeta = metav1.ObjectMeta{
			Namespace: cluster.GetNamespace(),
			Name:      joinTokenSecretName,
		}
		joinTokenSecret.Type = k8sv1.SecretTypeOpaque
		joinTokenSecret.Data = map[string][]byte{
			"token": []byte(joinToken),
		}
		err = client.Create(context.Background(), joinTokenSecret)
		if err != nil {
			return "", fmt.Errorf("error creating join token secret for cluster")
		}
	} else if err != nil {
		return "", fmt.Errorf("error getting join token for cluster: %v", err)
	}

	return string(joinTokenSecret.Data["token"]), nil
}
