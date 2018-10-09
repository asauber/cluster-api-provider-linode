docker build . -t kubeadm
# add this to your shell rc
echo -e '#!/bin/sh\ndocker run kubeadm "$@"' > /usr/local/bin/kubeadm
chmod +x /usr/local/bin/kubeadm 
