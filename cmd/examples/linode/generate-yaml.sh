#!/bin/bash

set -o errexit
set -o pipefail

die() { echo "$*" 1>&2 ; exit 1; }

[ "$#" -eq 2 ] || die "First argument must be a path to an ssh public key (for accessing Nodes of the cluster).
Second argument must be a name for the cluster, for example:

./generate-yaml.sh \$HOME/.ssh/id_rsa.pub cluster01"

[ -z "{$LINODE_TOKEN}" ] && die "\$LINODE_TOKEN must be set to a Linode API token"

PUBLIC_KEY=$(cat $1)
CLUSTER_NAME=$2
ENCODED_TOKEN=$(echo -n $LINODE_TOKEN | base64)

cat cluster.yaml.template |
sed -e "s|\$SSH_PUBLIC_KEY|$(cat $1)|" |
sed -e "s|\$LINODE_TOKEN|$ENCODED_TOKEN|" |
sed -e "s|\$CLUSTER_NAME|$CLUSTER_NAME|" > cluster.yaml

cat master.yaml.template |
sed -e "s|\$CLUSTER_NAME|$CLUSTER_NAME|" > master.yaml

cat nodes.yaml.template |
sed -e "s|\$CLUSTER_NAME|$CLUSTER_NAME|" > nodes.yaml

