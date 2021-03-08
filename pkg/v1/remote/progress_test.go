package remote

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

func TestWriteLayer_Progress(t *testing.T) {
	l, err := random.Layer(100000, types.OCIUncompressedLayer)
	if err != nil {
		t.Fatal(err)
	}
	c := make(chan v1.Update, 200)

	// Set up a fake registry.
	s := httptest.NewServer(registry.New())
	defer s.Close()
	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	dst := fmt.Sprintf("%s/test/progress/upload", u.Host)
	ref, err := name.ParseReference(dst)
	if err != nil {
		t.Fatal(err)
	}

	if err := WriteLayer(ref.Context(), l, WithProgress(c)); err != nil {
		t.Fatalf("WriteLayer: %v", err)
	}
	if err := checkUpdates(c); err != nil {
		t.Fatal(err)
	}
}

// TestWriteLayer_Progress_Exists tests progress reporting behavior when the
// layer already exists in the registry, so writes are skipped, but progress
// should still be reported in one update.
func TestWriteLayer_Progress_Exists(t *testing.T) {
	l, err := random.Layer(1000, types.OCILayer)
	if err != nil {
		t.Fatal(err)
	}
	c := make(chan v1.Update, 200)

	// Set up a fake registry.
	s := httptest.NewServer(registry.New())
	defer s.Close()
	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	dst := fmt.Sprintf("%s/test/progress/upload", u.Host)
	ref, err := name.ParseReference(dst)
	if err != nil {
		t.Fatal(err)
	}

	// Write the layer, so we can get updates when we write it again.
	if err := WriteLayer(ref.Context(), l); err != nil {
		t.Fatalf("WriteLayer: %v", err)
	}
	if err := WriteLayer(ref.Context(), l, WithProgress(c)); err != nil {
		t.Fatalf("WriteLayer: %v", err)
	}
	if err := checkUpdates(c); err != nil {
		t.Fatal(err)
	}
}

// TODO test for mounting layers
// TODO test for non-distributable layers
// TODO retry resets complete
// TODO test for failed upload

func TestWrite_Progress(t *testing.T) {
	img, err := random.Image(100000, 10)
	if err != nil {
		t.Fatal(err)
	}
	c := make(chan v1.Update, 200)

	// Set up a fake registry.
	s := httptest.NewServer(registry.New())
	defer s.Close()
	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	dst := fmt.Sprintf("%s/test/progress/upload", u.Host)
	ref, err := name.ParseReference(dst)
	if err != nil {
		t.Fatal(err)
	}

	if err := Write(ref, img, WithProgress(c)); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := checkUpdates(c); err != nil {
		t.Fatal(err)
	}
}

func TestWriteIndex_Progress(t *testing.T) {
	idx, err := random.Index(100000, 3, 10)
	if err != nil {
		t.Fatal(err)
	}
	c := make(chan v1.Update, 200)

	// Set up a fake registry.
	s := httptest.NewServer(registry.New())
	defer s.Close()
	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	dst := fmt.Sprintf("%s/test/progress/upload", u.Host)
	ref, err := name.ParseReference(dst)
	if err != nil {
		t.Fatal(err)
	}

	if err := WriteIndex(ref, idx, WithProgress(c)); err != nil {
		t.Fatalf("WriteIndex: %v", err)
	}

	if err := checkUpdates(c); err != nil {
		t.Fatal(err)
	}
}

func TestMultiWrite_Progress(t *testing.T) {
	idx, err := random.Index(100000, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	c := make(chan v1.Update, 1000)

	// Set up a fake registry.
	s := httptest.NewServer(registry.New())
	defer s.Close()
	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := name.ParseReference(fmt.Sprintf("%s/test/progress/upload", u.Host))
	if err != nil {
		t.Fatal(err)
	}
	ref2, err := name.ParseReference(fmt.Sprintf("%s/test/progress/upload:again", u.Host))
	if err != nil {
		t.Fatal(err)
	}

	if err := MultiWrite(map[name.Reference]Taggable{
		ref:  idx,
		ref2: idx,
	}, WithProgress(c)); err != nil {
		t.Fatalf("MultiWrite: %v", err)
	}

	if err := checkUpdates(c); err != nil {
		t.Fatal(err)
	}
}

func TestWriteLayer_Progress_Retry(t *testing.T) {

}

// checkUpdates checks that updates show steady progress toward a total, and
// don't describe errors.
func checkUpdates(updates <-chan v1.Update) error {
	var high, total int64
	for u := range updates {
		if u.Error != nil {
			return u.Error
		}

		if u.Total == 0 {
			return errors.New("saw zero total")
		}

		if total == 0 {
			total = u.Total
		} else if u.Total != total {
			return fmt.Errorf("total changed: was %d, saw %d", total, u.Total)
		}

		if u.Complete < high {
			return fmt.Errorf("saw progress revert: was high of %d, saw %d", high, u.Complete)
		}
		high = u.Complete
	}

	if high > total {
		return fmt.Errorf("final progress (%d) exceeded total (%d) by %d", high, total, high-total)
	} else if high < total {
		return fmt.Errorf("final progress (%d) did not reach total (%d) by %d", high, total, total-high)
	}

	return nil
}
