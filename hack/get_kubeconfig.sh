#!/bin/bash

die() { echo "$*" 1>&2 ; exit 1; }

[ "$#" -eq 1 ] || die "First argument must be a deployed cluster namespace"

IP1=$(linode-cli --text linodes list | grep "$1.*master" | awk '{ print $7 }')
IP2=$(linode-cli --text linodes list | grep "$1.*master" | awk '{ print $8 }')

if [[ IP1$ == 192.168* ]]
then
  IP=$IP2
else
  IP=$IP1
fi

echo $IP

scp root@$IP:/etc/kubernetes/admin.conf cluster.conf
