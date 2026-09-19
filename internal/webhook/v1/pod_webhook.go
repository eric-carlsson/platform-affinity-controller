/*
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
*/

package v1

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// nolint:unused
// log is for logging in this package.
var podlog = logf.Log.WithName("pod-resource")

// SetupPodWebhookWithManager registers the webhook for Pod in the manager.
func SetupPodWebhookWithManager(mgr ctrl.Manager, options PodWebhookOptions) error {
	return ctrl.NewWebhookManagedBy(mgr, &corev1.Pod{}).
		WithDefaulter(&PodCustomDefaulter{
			resolver:       options.Resolver,
			affinityMode:   options.AffinityMode,
			affinityWeight: int32(options.AffinityWeight),
			targetArch:     options.TargetArch,
			targetOS:       options.TargetOS,
		}).
		Complete()
}

// PodWebhookOptions configures the Pod mutating webhook.
type PodWebhookOptions struct {
	Resolver       ImagePlatformResolver
	AffinityMode   AffinityMode
	AffinityWeight int
	TargetArch     string
	TargetOS       string
}

// DefaultPodWebhookOptions returns safe defaults for the Pod mutating webhook.
func DefaultPodWebhookOptions(ctx context.Context) (PodWebhookOptions, error) {
	resolver, err := NewCachedImagePlatformResolver(ctx, 10*time.Minute, 5*time.Second, 1_000)
	if err != nil {
		return PodWebhookOptions{}, err
	}

	return PodWebhookOptions{
		Resolver:       resolver,
		AffinityMode:   AffinityModePreferred,
		AffinityWeight: 100,
		TargetArch:     "amd64",
		TargetOS:       "linux",
	}, nil
}

// +kubebuilder:webhook:path=/mutate--v1-pod,mutating=true,failurePolicy=ignore,sideEffects=None,groups="",resources=pods,verbs=create;update,versions=v1,name=mpod-v1.kb.io,admissionReviewVersions=v1

// PodCustomDefaulter mutates Pods whose images all support the target platform.
type PodCustomDefaulter struct {
	resolver       ImagePlatformResolver
	affinityMode   AffinityMode
	affinityWeight int32
	targetArch     string
	targetOS       string
}

// Default implements webhook.CustomDefaulter. Lookup failures are logged and
// allowed through unchanged so registry availability never blocks scheduling.
func (d *PodCustomDefaulter) Default(ctx context.Context, pod *corev1.Pod) error {
	if d.resolver == nil || (d.targetArch == "" && d.targetOS == "") {
		podlog.Info("Skipping Pod platform affinity because the webhook is not configured",
			"name", pod.Name, "namespace", pod.Namespace)
		return nil
	}

	images := collectPodImages(pod)
	if len(images) == 0 {
		return nil
	}
	for _, image := range images {
		platforms, err := d.resolver.Platforms(ctx, image)
		if err != nil {
			podlog.Error(err, "Could not resolve image platforms; leaving Pod unchanged",
				"name", pod.Name, "namespace", pod.Namespace, "image", image)
			return nil
		}
		if !containsTargetPlatform(platforms, d.targetArch, d.targetOS) {
			podlog.Info("Leaving Pod unchanged because an image does not support the target platform",
				"name", pod.Name, "namespace", pod.Namespace, "image", image,
				"architecture", d.targetArch, "os", d.targetOS)
			return nil
		}
	}

	if addPlatformAffinity(pod, d.affinityMode, d.affinityWeight, d.targetArch, d.targetOS) {
		podlog.Info("Added Pod platform affinity",
			"name", pod.Name, "namespace", pod.Namespace,
			"architecture", d.targetArch, "os", d.targetOS, "mode", d.affinityMode)
	}
	return nil
}

// containsTargetPlatform reports whether any resolved platform satisfies the
// configured target architecture and OS. An empty target value matches any
// platform value for that dimension.
func containsTargetPlatform(platforms []Platform, targetArch, targetOS string) bool {
	for _, platform := range platforms {
		if targetArch != "" && platform.Architecture != targetArch {
			continue
		}
		if targetOS != "" && platform.OS != targetOS {
			continue
		}
		return true
	}
	return false
}

func collectPodImages(pod *corev1.Pod) []string {
	images := make([]string, 0, len(pod.Spec.Containers)+len(pod.Spec.InitContainers)+len(pod.Spec.EphemeralContainers))
	seen := make(map[string]struct{}, cap(images))
	for _, container := range pod.Spec.InitContainers {
		images = appendImage(images, seen, container.Image)
	}
	for _, container := range pod.Spec.Containers {
		images = appendImage(images, seen, container.Image)
	}
	for _, container := range pod.Spec.EphemeralContainers {
		images = appendImage(images, seen, container.Image)
	}
	return images
}

func appendImage(images []string, seen map[string]struct{}, image string) []string {
	if image == "" {
		return images
	}
	if _, ok := seen[image]; ok {
		return images
	}
	seen[image] = struct{}{}
	return append(images, image)
}
