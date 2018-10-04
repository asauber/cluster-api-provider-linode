version=1.0.4 # latest stable version
arch=amd64

# download the release
curl -L -O https://github.com/kubernetes-sigs/kubebuilder/releases/download/v${version}/kubebuilder_${version}_darwin_${arch}.tar.gz

# extract the archive
tar -zxvf kubebuilder_${version}_darwin_${arch}.tar.gz
sudo mv kubebuilder_${version}_darwin_${arch} /usr/local/kubebuilder

# update your PATH to include /usr/local/kubebuilder/bin
# add this to your shell rc!
export PATH=$PATH:/usr/local/kubebuilder/bin
