package docker

import (
	"errors"

	"github.com/displague/cluster-api-provider-linode/pkg/docker/ubuntu"
)

var (
	// ErrOSNotFound is returned when there is no Docker version candidate for given OS.
	// In that case, user should manually handle Docker version matching in the bootstrap script.
	ErrOSNotFound = errors.New("no docker version for the given os found")

	// providers is list of supported provides by the controller.
	providers = map[string]RuntimeVersion{
		"linode/ubuntu16.04lts": ubuntu.RuntimeVersion{},
		"linode/ubuntu18.04":    ubuntu.RuntimeVersion{},
	}
)

// ForOS returns preferred version for given OS image.
func ForOS(os string) (RuntimeVersion, error) {
	if p, found := providers[os]; found {
		return p, nil
	}
	return nil, ErrOSNotFound
}

// RuntimeVersion interface contains function for matching preferred Docker version based on Kubelet version and OS image.
type RuntimeVersion interface {
	GetDockerInstallCandidate(kubernetesVersion string) (string, string, error)
}
