package remote

import (
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

func TestWriteLayer_Progress_NotExists(t *testing.T) {
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

// TODO mount WriteLayer
// TODO retry resets complete
// TODO non-distributable layers

func TestWrite_Progress_NotExists(t *testing.T) {
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

// TODO: WriteIndex calls Write multiple times, which closes o.updates each time, causing panic.
func TestWriteIndex_Progress_NotExists(t *testing.T) {
	/*
		idx, err := random.Index(100000, 10, 10)
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
	*/
}

// checkUpdates checks that updates show steady progress toward a total, and
// don't describe errors.
func checkUpdates(updates <-chan v1.Update) error {
	var high, total int64
	for u := range updates {
		if u.Error != nil {
			return u.Error
		}

		if total == 0 {
			total = u.Total
		} else if u.Total != total {
			return fmt.Errorf("total changed: was %d, saw %d", total, u.Total)
		}

		if u.Complete <= high {
			return fmt.Errorf("saw progress revert: was high of %d, saw %d", high, u.Complete)
		}
		high = u.Complete
	}

	if high != total {
		return fmt.Errorf("final progress (%d) did not reach total (%d)", high, total)
	}

	return nil
}
