package cache

import (
	"io"
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/random"
)

func TestFilesystemCache(t *testing.T) {
	dir, err := ioutil.TempDir("", "ggcr-cache")
	if err != nil {

	}
	defer os.RemoveAll(dir)

	numLayers := 5
	img, err := random.Image(10, int64(numLayers))
	if err != nil {
		t.Fatalf("random.Image: %v", err)
	}
	c := NewFilesystemCache(dir)
	img = NewImage(img, c)

	// Read all the layers to populate the cache.
	ls, err := img.Layers()
	if err != nil {
		t.Fatalf("Layers: %v", err)
	}
	for i, l := range ls {
		h, err := l.Digest()
		if err != nil {
			t.Fatalf("layer[%d].Digest: %v", i, err)
		}
		l, err = img.LayerByDigest(h)
		if err != nil {
			t.Fatalf("LayerByDigest(%q): %v", h, err)
		}
		rc, err := l.Compressed()
		if err != nil {
			t.Fatalf("layer[%d].Compressed: %v", i, err)
		}
		if _, err := io.Copy(ioutil.Discard, rc); err != nil {
			t.Fatalf("Error reading contents: %v", err)
		}
		rc.Close()
	}

	// Check that layers exist in the fs cache.
	files, err := ioutil.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if got, want := len(files), numLayers; got != want {
		t.Errorf("Got %d cached files, want %d", got, want)
	}
	for _, fi := range files {
		if fi.Size() == 0 {
			t.Errorf("Cached file %q is empty", fi.Name())
		}
	}

	// Read all the layers again, this time from the cache.
	// Check that layers are equal.
	ls2, err := img.Layers()
	if err != nil {
		t.Fatalf("Layers: %v", err)
	}
	for i, l := range ls2 {
		h, err := l.Digest()
		if err != nil {
			t.Fatalf("layer[%d].Digest: %v", i, err)
		}
		l, err = img.LayerByDigest(h)
		if err != nil {
			t.Fatalf("LayerByDigest(%q): %v", h, err)
		}
	}
	if !reflect.DeepEqual(ls, ls2) {
		t.Errorf("Got different layers from cached image")
	}

	// Delete a cached layer, see it disappear.
	l := ls[0]
	h, err := l.Digest()
	if err != nil {
		t.Fatalf("layer.Digest: %v", err)
	}
	if err := c.Delete(h); err != nil {
		t.Errorf("cache.Delete: %v", err)
	}
	files, err = ioutil.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if got, want := len(files), numLayers-1; got != want {
		t.Errorf("Got %d cached files, want %d", got, want)
	}

	// Read the image again, see the layer reappear.
	for i, l := range ls {
		h, err := l.Digest()
		if err != nil {
			t.Fatalf("layer[%d].Digest: %v", i, err)
		}
		l, err = img.LayerByDigest(h)
		if err != nil {
			t.Fatalf("LayerByDigest(%q): %v", h, err)
		}
		rc, err := l.Compressed()
		if err != nil {
			t.Fatalf("layer[%d].Compressed: %v", i, err)
		}
		if _, err := io.Copy(ioutil.Discard, rc); err != nil {
			t.Fatalf("Error reading contents: %v", err)
		}
		rc.Close()
	}

	// Check that layers exist in the fs cache.
	files, err = ioutil.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if got, want := len(files), numLayers; got != want {
		t.Errorf("Got %d cached files, want %d", got, want)
	}
	for _, fi := range files {
		if fi.Size() == 0 {
			t.Errorf("Cached file %q is empty", fi.Name())
		}
	}
}
