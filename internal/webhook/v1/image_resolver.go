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
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/authn/k8schain"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// Platform identifies one OS/architecture combination offered by a
// multi-platform image.
type Platform struct {
	Architecture string
	OS           string
}

// ImagePlatformResolver resolves the platforms offered by multi-platform images.
type ImagePlatformResolver interface {
	Platforms(context.Context, string) ([]Platform, error)
}

type cacheEntry struct {
	platforms []Platform
	expiresAt time.Time
}

// CachedImagePlatformResolver resolves image indexes using a Kubernetes-aware
// cloud-provider keychain and caches successful results by image reference and digest.
type CachedImagePlatformResolver struct {
	cache      map[string]cacheEntry
	cacheTTL   time.Duration
	keychain   authn.Keychain
	maxEntries int
	timeout    time.Duration
	mu         sync.Mutex
}

// NewCachedImagePlatformResolver constructs a registry resolver with bounded caching.
func NewCachedImagePlatformResolver(ctx context.Context, cacheTTL, timeout time.Duration, maxEntries int) (*CachedImagePlatformResolver, error) {
	keychain, err := k8schain.NewNoClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("create registry keychain: %w", err)
	}

	return &CachedImagePlatformResolver{
		cache:      make(map[string]cacheEntry),
		cacheTTL:   cacheTTL,
		keychain:   keychain,
		maxEntries: maxEntries,
		timeout:    timeout,
	}, nil
}

// Platforms returns the platforms advertised by an OCI image index. A
// non-index image returns an empty list without an error and must not be
// mutated.
func (r *CachedImagePlatformResolver) Platforms(ctx context.Context, image string) ([]Platform, error) {
	if platforms, ok := r.get(image); ok {
		return platforms, nil
	}

	ref, err := name.ParseReference(image)
	if err != nil {
		return nil, fmt.Errorf("parse image reference %q: %w", image, err)
	}
	if digestRef, ok := ref.(name.Digest); ok {
		if platforms, ok := r.get(digestRef.Name()); ok {
			return platforms, nil
		}
	}

	requestCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	descriptor, err := remote.Get(ref, remote.WithAuthFromKeychain(r.keychain), remote.WithContext(requestCtx))
	if err != nil {
		return nil, fmt.Errorf("get image descriptor for %q: %w", image, err)
	}
	if !descriptor.MediaType.IsIndex() {
		return []Platform{}, nil
	}

	index, err := descriptor.ImageIndex()
	if err != nil {
		return nil, fmt.Errorf("read image index for %q: %w", image, err)
	}
	manifest, err := index.IndexManifest()
	if err != nil {
		return nil, fmt.Errorf("read image index manifest for %q: %w", image, err)
	}

	platforms := make([]Platform, 0, len(manifest.Manifests))
	seen := make(map[Platform]struct{}, len(manifest.Manifests))
	for _, item := range manifest.Manifests {
		if item.Platform == nil || item.Platform.Architecture == "" || item.Platform.OS == "" {
			continue
		}
		platform := Platform{Architecture: item.Platform.Architecture, OS: item.Platform.OS}
		if _, ok := seen[platform]; ok {
			continue
		}
		seen[platform] = struct{}{}
		platforms = append(platforms, platform)
	}

	r.put(image, platforms)
	r.put(ref.Context().Name()+"@"+descriptor.Digest.String(), platforms)

	return platforms, nil
}

func (r *CachedImagePlatformResolver) get(key string) ([]Platform, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, ok := r.cache[key]
	if !ok || time.Now().After(entry.expiresAt) {
		if ok {
			delete(r.cache, key)
		}
		return nil, false
	}

	return slices.Clone(entry.platforms), true
}

func (r *CachedImagePlatformResolver) put(key string, platforms []Platform) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.cache) >= r.maxEntries {
		for cacheKey, entry := range r.cache {
			if time.Now().After(entry.expiresAt) {
				delete(r.cache, cacheKey)
			}
		}
	}
	if len(r.cache) >= r.maxEntries {
		for cacheKey := range r.cache {
			delete(r.cache, cacheKey)
			break
		}
	}

	r.cache[key] = cacheEntry{
		platforms: slices.Clone(platforms),
		expiresAt: time.Now().Add(r.cacheTTL),
	}
}
