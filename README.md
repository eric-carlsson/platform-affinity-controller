# platform-affinity-controller

A Kubernetes admission controller for automatically placing workloads with
multi-platform images on nodes with a preferred platform.

## Description

A multi-platform container image is a single image that contains multiple
software distributions for different platforms (CPU architecture and OS). Most
container runtimes support multi-platform container images, and will
automatically run the version that corresponds to the host platform.

The Kubernetes scheduler is not aware of what platforms a pod's images support
when it schedules it. The `kubernetes.io/arch` and `kubernetes.io/os` labels
can be used in node selectors, but adding these for every workload quickly
becomes tedius.

This admission controller automates that process. It inspects regular, init, and
ephemeral container images in Pods, and adds node affinity selectors for
`kubernetes.io/arch` and/or `kubernetes.io/os` only when every image
supports a configured target value. Single-platform images, images that do not
support the target, and other errors leave the Pod unchanged.

```mermaid
flowchart TB
  subgraph After["After: affinity added"]
    direction TB
    podA["Pod<br/>+ nodeAffinity: arch=arm64"]
    schedA["Scheduler"]
    amd64A["amd64 node"]
    arm64A["arm64 node"]

    podA --> schedA
    schedA -->|schedules| arm64A
    schedA -.->|excluded| amd64A
  end

  subgraph Before["Before: no affinity"]
    direction TB
    podB["Pod<br/>(multi-platform image)"]
    schedB["Scheduler"]
    amd64B["amd64 node"]
    arm64B["arm64 node"]

    podB --> schedB
    schedB -->|schedules| amd64B
    schedB -->|schedules| arm64B
  end

  classDef excluded stroke-dasharray: 5 5,opacity:0.5;
  class amd64A excluded
```

Without the admission controller, the scheduler places the Pod on any node
regardless of which platforms its images actually support—an amd64-only image
can be scheduled on an arm64 node and fail at runtime.

With the webhook, node affinity constrains (or, in `preferred` mode, biases)
scheduling to nodes matching the image's supported platforms.

## Getting Started

### Prerequisites

- go version v1.24.6+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster.

### To Deploy on the cluster

**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/platform-affinity-controller:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/platform-affinity-controller:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
> privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

> **NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall

**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following the options to release and provide this solution to the users.

### By providing a bundle with all YAML files

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/platform-affinity-controller:tag
```

**NOTE:** The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without its
dependencies.

2. Using the installer

Users can just run 'kubectl apply -f <URL for YAML BUNDLE>' to install
the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/platform-affinity-controller/<tag or branch>/dist/install.yaml
```

### By providing a Helm Chart

1. Build the chart using the optional helm plugin

```sh
kubebuilder edit --plugins=helm/v2-alpha
```

2. See that a chart was generated under 'dist/chart', and users
   can obtain this solution from there.

**NOTE:** If you change the project, you need to update the Helm Chart
using the same command above to sync the latest changes. Furthermore,
if you create webhooks, you need to use the above command with
the '--force' flag and manually ensure that any custom configuration
previously added to 'dist/chart/values.yaml' or 'dist/chart/manager/manager.yaml'
is manually re-applied afterwards.

## Contributing

// TODO(user): Add detailed information on how you would like others to contribute to this project

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
