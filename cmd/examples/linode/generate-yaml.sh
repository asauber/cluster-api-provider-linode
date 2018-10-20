#!/bin/bash

set -o errexit
set -o pipefail

die() { echo "$*" 1>&2 ; exit 1; }

[ "$#" -eq 1 ] || die "First argument must be a path to an ssh public key (for accessing nodes of the cluster), for example:

./generate-yaml.sh \$HOME/.ssh/id_rsa.pub"

[ -z "{$LINODE_TOKEN}" ] && die "\$LINODE_TOKEN must be set to a Linode API token"

PUBLIC_KEY=$(cat $1)
ENCODED_TOKEN=$(echo -n $LINODE_TOKEN | base64)
cat cluster.yaml.template | sed -e "s|\$SSH_PUBLIC_KEY|$(cat $1)|" | sed -e "s|\$LINODE_TOKEN|$ENCODED_TOKEN|" > cluster.yaml
