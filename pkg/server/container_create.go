/*
Copyright 2017 The Kubernetes Authors.

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

package server

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	"golang.org/x/net/context"

	"k8s.io/kubernetes/pkg/kubelet/api/v1alpha1/runtime"

	"github.com/kubernetes-incubator/cri-containerd/pkg/metadata"
)

// CreateContainer creates a new container in the given PodSandbox.
func (c *criContainerdService) CreateContainer(ctx context.Context, r *runtime.CreateContainerRequest) (retRes *runtime.CreateContainerResponse, retErr error) {
	glog.V(2).Infof("CreateContainer within sandbox %q with container config %+v and sandbox config %+v",
		r.GetPodSandboxId(), r.GetConfig(), r.GetSandboxConfig())
	defer func() {
		if retErr == nil {
			glog.V(2).Infof("CreateContainer returns container id %q", retRes.GetContainerId())
		}
	}()

	config := r.GetConfig()
	sandboxConfig := r.GetSandboxConfig()
	sandbox, err := c.getSandbox(r.GetPodSandboxId())
	if err != nil {
		return nil, fmt.Errorf("failed to find sandbox id %q: %v", r.GetPodSandboxId(), err)
	}

	// Generate unique id and name for the container and reserve the name.
	// Reserve the container name to avoid concurrent `CreateContainer` request creating
	// the same container.
	id := generateID()
	name := makeContainerName(config.GetMetadata(), sandboxConfig.GetMetadata())
	if err := c.containerNameIndex.Reserve(name, id); err != nil {
		return nil, fmt.Errorf("failed to reserve container name %q: %v", name, err)
	}
	defer func() {
		// Release the name if the function returns with an error.
		if retErr != nil {
			c.containerNameIndex.ReleaseByName(name)
		}
	}()

	// Create initial container metadata.
	meta := metadata.ContainerMetadata{
		ID:        id,
		Name:      name,
		SandboxID: sandbox.ID,
		Config:    config,
	}

	// TODO(random-liu): [P0] Prepare container rootfs.

	// TODO(random-liu): [P0] Set ImageRef in ContainerMetadata with image id.

	// Create container root directory.
	containerRootDir := getContainerRootDir(c.rootDir, id)
	if err := c.os.MkdirAll(containerRootDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create container root directory %q: %v",
			containerRootDir, err)
	}
	defer func() {
		if retErr != nil {
			// Cleanup the container root directory.
			if err := c.os.RemoveAll(containerRootDir); err != nil {
				glog.Errorf("Failed to remove container root directory %q: %v",
					containerRootDir, err)
			}
		}
	}()

	// Update container CreatedAt.
	meta.CreatedAt = time.Now().UnixNano()
	// Add container into container store.
	if err := c.containerStore.Create(meta); err != nil {
		return nil, fmt.Errorf("failed to add container metadata %+v into store: %v",
			meta, err)
	}

	return &runtime.CreateContainerResponse{ContainerId: id}, nil
}
