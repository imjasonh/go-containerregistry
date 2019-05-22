package cache

import (
	"errors"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
)

// TestCache tests that the cache is populated when LayerByDigest is called.
func TestCache(t *testing.T) {
	var numLayers int64 = 5
	img, err := random.Image(10, numLayers)
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}
	m := &memcache{map[v1.Hash]v1.Layer{}}
	img = NewImage(img, m)

	// Cache is empty.
	if len(m.m) != 0 {
		t.Errorf("Before consuming, cache is non-empty: %+v", m.m)
	}

	// Consume each layer, cache gets populated.
	ls, err := img.Layers()
	if err != nil {
		t.Fatalf("Layers: %v", err)
	}
	for i, l := range ls {
		h, err := l.Digest()
		if err != nil {
			t.Fatalf("layer.Digest: %v", err)
		}
		if _, err := img.LayerByDigest(h); err != nil {
			t.Fatalf("LayerByDigest: %v", err)
		}
		if got, want := len(m.m), i+1; got != want {
			t.Errorf("Cache has %d entries, want %d", got, want)
		}
	}
}

// TestCacheShortCircuit tests that if a layer is found in the cache,
// LayerByDigest is not called in the underlying Image implementation.
func TestCacheShortCircuit(t *testing.T) {
	l := &fakeLayer{}
	m := &memcache{map[v1.Hash]v1.Layer{
		fakeHash: l,
	}}
	img := NewImage(&fakeImage{}, m)

	for i := 0; i < 10; i++ {
		if _, err := img.LayerByDigest(fakeHash); err != nil {
			t.Errorf("LayerByDigest[%d]: %v", i, err)
		}
	}
}

var fakeHash = v1.Hash{Algorithm: "fake", Hex: "data"}

type fakeLayer struct{ v1.Layer }
type fakeImage struct{ v1.Image }

func (f *fakeImage) LayerByDigest(v1.Hash) (v1.Layer, error) {
	return nil, errors.New("LayerByDigest was called")
}

// memcache is an in-memory Cache implementation.
//
// It doesn't intend to actually write layer data, it just keeps a reference
// to the original Layer.
type memcache struct {
	m map[v1.Hash]v1.Layer
}

func (m *memcache) Put(l v1.Layer) (v1.Layer, error) {
	h, err := l.Digest()
	if err != nil {
		return nil, err
	}
	m.m[h] = l
	return l, nil
}

func (m *memcache) Get(h v1.Hash) (v1.Layer, error) {
	l, found := m.m[h]
	if !found {
		return nil, ErrNotFound
	}
	return l, nil
}

func (m *memcache) Delete(h v1.Hash) error {
	delete(m.m, h)
	return nil
}
