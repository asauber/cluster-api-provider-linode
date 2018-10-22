#!/bin/bash

die() { echo "$*" 1>&2 ; exit 1; }

[ "$#" -eq 1 ] || die "First argument must be a deployed cluster namespace"

linode-cli --text linodes list | grep $1 | awk '{ print $1 }' | xargs -L 1 linode-cli linodes delete
