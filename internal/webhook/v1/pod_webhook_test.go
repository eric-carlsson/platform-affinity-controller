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
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("Pod Webhook", func() {
	var (
		defaulter PodCustomDefaulter
	)

	BeforeEach(func() {
		defaulter = PodCustomDefaulter{
			resolver: fakeImagePlatformResolver{
				platforms: map[string][]Platform{
					"registry.example/app:latest":   {{Architecture: "amd64", OS: "linux"}, {Architecture: "arm64", OS: "linux"}},
					"registry.example/init:latest":  {{Architecture: "amd64", OS: "linux"}, {Architecture: "arm64", OS: "linux"}},
					"registry.example/debug:latest": {{Architecture: "amd64", OS: "linux"}, {Architecture: "arm64", OS: "linux"}},
				},
			},
			affinityMode:   AffinityModePreferred,
			affinityWeight: 100,
			targetArch:     "arm64",
		}
		Expect(defaulter).NotTo(BeNil(), "Expected defaulter to be initialized")
	})

	Context("When creating Pod under Defaulting Webhook", func() {
		It("adds a preferred architecture affinity when all images support it", func() {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{{Image: "registry.example/init:latest"}},
					Containers:     []corev1.Container{{Image: "registry.example/app:latest"}},
					EphemeralContainers: []corev1.EphemeralContainer{{
						EphemeralContainerCommon: corev1.EphemeralContainerCommon{Image: "registry.example/debug:latest"},
					}},
				},
			}

			Expect(defaulter.Default(context.Background(), pod)).To(Succeed())

			preferences := pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution
			Expect(preferences).To(HaveLen(1))
			Expect(preferences[0].Weight).To(Equal(int32(100)))
			Expect(preferences[0].Preference.MatchExpressions).To(ConsistOf(corev1.NodeSelectorRequirement{
				Key: architectureLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"arm64"},
			}))
		})

		It("does not mutate a Pod containing a single-platform image", func() {
			defaulter.resolver = fakeImagePlatformResolver{
				platforms: map[string][]Platform{
					"registry.example/app:latest":    {{Architecture: "arm64", OS: "linux"}},
					"registry.example/legacy:latest": nil,
				},
			}
			pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{
				{Image: "registry.example/app:latest"},
				{Image: "registry.example/legacy:latest"},
			}}}

			Expect(defaulter.Default(context.Background(), pod)).To(Succeed())

			Expect(pod.Spec.Affinity).To(BeNil())
		})

		It("allows a Pod through unchanged when registry lookup fails", func() {
			defaulter.resolver = fakeImagePlatformResolver{err: errors.New("registry unavailable")}
			pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Image: "registry.example/app:latest"}}}}

			Expect(defaulter.Default(context.Background(), pod)).To(Succeed())

			Expect(pod.Spec.Affinity).To(BeNil())
		})

		It("does not add duplicate preferred affinity on update", func() {
			pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Image: "registry.example/app:latest"}}}}

			Expect(defaulter.Default(context.Background(), pod)).To(Succeed())
			Expect(defaulter.Default(context.Background(), pod)).To(Succeed())

			Expect(pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution).To(HaveLen(1))
		})

		It("adds a required architecture constraint to every existing selector term", func() {
			defaulter.affinityMode = AffinityModeRequired
			pod := &corev1.Pod{Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Image: "registry.example/app:latest"}},
				Affinity: &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{MatchExpressions: []corev1.NodeSelectorRequirement{{Key: "topology.kubernetes.io/zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"a"}}}},
							{MatchExpressions: []corev1.NodeSelectorRequirement{{Key: "topology.kubernetes.io/zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"b"}}}},
						},
					},
				}},
			}}

			Expect(defaulter.Default(context.Background(), pod)).To(Succeed())

			for _, term := range pod.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
				Expect(term.MatchExpressions).To(ContainElement(corev1.NodeSelectorRequirement{
					Key: architectureLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"arm64"},
				}))
			}
		})

		It("adds both architecture and OS requirements when both are configured", func() {
			defaulter.targetOS = "linux"
			pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Image: "registry.example/app:latest"}}}}

			Expect(defaulter.Default(context.Background(), pod)).To(Succeed())

			preferences := pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution
			Expect(preferences).To(HaveLen(1))
			Expect(preferences[0].Preference.MatchExpressions).To(ConsistOf(
				corev1.NodeSelectorRequirement{Key: architectureLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"arm64"}},
				corev1.NodeSelectorRequirement{Key: osLabel, Operator: corev1.NodeSelectorOpIn, Values: []string{"linux"}},
			))
		})

		It("does not mutate a Pod when an image lacks the target OS", func() {
			defaulter.targetOS = "windows"
			pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Image: "registry.example/app:latest"}}}}

			Expect(defaulter.Default(context.Background(), pod)).To(Succeed())

			Expect(pod.Spec.Affinity).To(BeNil())
		})

		It("uses the configured affinity weight", func() {
			defaulter.affinityWeight = 42
			pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Image: "registry.example/app:latest"}}}}

			Expect(defaulter.Default(context.Background(), pod)).To(Succeed())

			Expect(pod.Spec.Affinity.NodeAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].Weight).To(Equal(int32(42)))
		})

	})

})

type fakeImagePlatformResolver struct {
	platforms map[string][]Platform
	err       error
}

func (r fakeImagePlatformResolver) Platforms(_ context.Context, image string) ([]Platform, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.platforms[image], nil
}
