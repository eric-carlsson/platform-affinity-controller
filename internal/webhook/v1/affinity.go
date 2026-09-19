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
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	architectureLabel = "kubernetes.io/arch"
	osLabel           = "kubernetes.io/os"
)

// AffinityMode determines whether the target architecture is preferred or required.
type AffinityMode string

const (
	AffinityModePreferred AffinityMode = "preferred"
	AffinityModeRequired  AffinityMode = "required"
)

func (m *AffinityMode) Set(value string) error {
	mode := AffinityMode(strings.ToLower(value))
	switch mode {
	case AffinityModePreferred, AffinityModeRequired:
		*m = mode
		return nil
	default:
		return fmt.Errorf("must be one of: %q, %q", AffinityModePreferred, AffinityModeRequired)
	}
}

func (m *AffinityMode) String() string {
	return string(*m)
}

// addPlatformAffinity adds node affinity for the given architecture and/or OS.
// Either value may be empty to omit that requirement. It returns true if the
// Pod's affinity was changed.
func addPlatformAffinity(pod *corev1.Pod, mode AffinityMode, weight int32, architecture, os string) bool {
	requirements := platformRequirements(architecture, os)
	if len(requirements) == 0 {
		return false
	}

	if pod.Spec.Affinity == nil {
		pod.Spec.Affinity = &corev1.Affinity{}
	}
	if pod.Spec.Affinity.NodeAffinity == nil {
		pod.Spec.Affinity.NodeAffinity = &corev1.NodeAffinity{}
	}

	switch mode {
	case AffinityModePreferred:
		return addPreferredPlatformAffinity(pod.Spec.Affinity.NodeAffinity, weight, requirements)
	case AffinityModeRequired:
		return addRequiredPlatformAffinity(pod.Spec.Affinity.NodeAffinity, requirements)
	default:
		return false
	}
}

// platformRequirements builds the node selector requirements for the given
// architecture and/or OS, omitting whichever value is empty.
func platformRequirements(architecture, os string) []corev1.NodeSelectorRequirement {
	var requirements []corev1.NodeSelectorRequirement
	if architecture != "" {
		requirements = append(requirements, corev1.NodeSelectorRequirement{
			Key:      architectureLabel,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{architecture},
		})
	}
	if os != "" {
		requirements = append(requirements, corev1.NodeSelectorRequirement{
			Key:      osLabel,
			Operator: corev1.NodeSelectorOpIn,
			Values:   []string{os},
		})
	}
	return requirements
}

func addPreferredPlatformAffinity(affinity *corev1.NodeAffinity, weight int32, requirements []corev1.NodeSelectorRequirement) bool {
	for _, preference := range affinity.PreferredDuringSchedulingIgnoredDuringExecution {
		if containsAllRequirements(preference.Preference.MatchExpressions, requirements) {
			return false
		}
	}

	switch {
	case weight < 1:
		weight = 1
	case weight > 100:
		weight = 100
	}

	affinity.PreferredDuringSchedulingIgnoredDuringExecution = append(
		affinity.PreferredDuringSchedulingIgnoredDuringExecution,
		corev1.PreferredSchedulingTerm{
			Weight: weight,
			Preference: corev1.NodeSelectorTerm{
				MatchExpressions: append([]corev1.NodeSelectorRequirement(nil), requirements...),
			},
		},
	)
	return true
}

func addRequiredPlatformAffinity(affinity *corev1.NodeAffinity, requirements []corev1.NodeSelectorRequirement) bool {
	if affinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		affinity.RequiredDuringSchedulingIgnoredDuringExecution = &corev1.NodeSelector{
			NodeSelectorTerms: []corev1.NodeSelectorTerm{
				{MatchExpressions: append([]corev1.NodeSelectorRequirement(nil), requirements...)},
			},
		}
		return true
	}
	if len(affinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms) == 0 {
		return false
	}

	changed := false
	for i := range affinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms {
		term := &affinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[i]
		for _, requirement := range requirements {
			if hasRequirement(term.MatchExpressions, requirement) {
				continue
			}
			term.MatchExpressions = append(term.MatchExpressions, requirement)
			changed = true
		}
	}
	return changed
}

// containsAllRequirements reports whether every requirement in want is
// present in existing.
func containsAllRequirements(existing, want []corev1.NodeSelectorRequirement) bool {
	for _, requirement := range want {
		if !hasRequirement(existing, requirement) {
			return false
		}
	}
	return true
}

func hasRequirement(requirements []corev1.NodeSelectorRequirement, expected corev1.NodeSelectorRequirement) bool {
	for _, requirement := range requirements {
		if requirement.Key != expected.Key || requirement.Operator != expected.Operator || len(requirement.Values) != len(expected.Values) {
			continue
		}
		matches := true
		for i := range requirement.Values {
			if requirement.Values[i] != expected.Values[i] {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}
