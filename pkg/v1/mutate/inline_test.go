package mutate

import (
	"bytes"
	"io/ioutil"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/stream"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

func TestInlineData(t *testing.T) {
	// Even this small config file becomes ~470 bytes when diffids for
	// layers get appended, so we set the inline threshold to 500 bytes so
	// we can exercise config inlining.
	img, err := Config(empty.Image, v1.Config{
		Cmd: []string{"hello", "world"},
	})
	img, err = AppendLayers(img,
		static.NewLayer(bytes.Repeat([]byte("."), 100), types.DockerLayer),
		static.NewLayer(bytes.Repeat([]byte("o"), 100), types.DockerLayer),
		static.NewLayer(bytes.Repeat([]byte("0"), 600), types.DockerLayer))
	if err != nil {
		t.Fatalf("AppendLayers: %v", err)
	}
	orig, err := img.Manifest()
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}

	got, err := InlineData(img, 500)
	if err != nil {
		t.Fatalf("InlineData: %v", err)
	}

	wantData := []string{
		"Li4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4uLi4u",
		"b29vb29vb29vb29vb29vb29vb29vb29vb29vb29vb29vb29vb29vb29vb29vb29vb29vb29vb29vb29vb29vb29vb29vb29vb29vb29vb29vb29vb29vb29vb29vb29vb29v",
	}
	mf, err := got.Manifest()
	if err != nil {
		t.Fatalf("Manifest: %v", err)
	}
	for i, desc := range mf.Layers {
		switch {
		case i == 2:
			if desc.Data != nil {
				t.Errorf("layer %d: over threshold should have no data: got %q", i, desc.Data)
			}
		case i < 2:
			if string(desc.Data) != wantData[i] {
				t.Errorf("layer %d: got %q, want %q", i, string(desc.Data), wantData[i])
			}
		case i > 3:
			t.Errorf("unexpected layer %d", i)
		}

		// Not considering Data, descriptors should be equivalent.
		desc.Data = nil
		if d := cmp.Diff(orig.Layers[i], desc); d != "" {
			t.Errorf("layer %d: descriptor diff (-was,+now): %s", i, d)
		}
	}

	wantConfigData := "eyJhcmNoaXRlY3R1cmUiOiIiLCJjcmVhdGVkIjoiMDAwMS0wMS0wMVQwMDowMDowMFoiLCJoaXN0b3J5IjpbeyJjcmVhdGVkIjoiMDAwMS0wMS0wMVQwMDowMDowMFoifSx7ImNyZWF0ZWQiOiIwMDAxLTAxLTAxVDAwOjAwOjAwWiJ9LHsiY3JlYXRlZCI6IjAwMDEtMDEtMDFUMDA6MDA6MDBaIn1dLCJvcyI6IiIsInJvb3RmcyI6eyJ0eXBlIjoibGF5ZXJzIiwiZGlmZl9pZHMiOlsic2hhMjU2OmQ0ZDc5YjE0NmJhMjAzYmUwYmYyZmUwYTg2ZjMwYjQ2OGQxOGVjZmNhM2QxNjA1ZmQ0YWNhMmU4OGZkYzY1ZDkiLCJzaGEyNTY6MjkwYjRmOGI2YTU2ODk1MGQxMDY3ZGMwZjQ3M2UxOGFhMGE5ZDVlZDQwYzkzNGEzNjQ2NzMyNDBhNmNmNGMyMSIsInNoYTI1Njo5ZWI5NTQ3Mzg0ZTM1YzlmOGQzMTdhOTEzOTViMDZiYTY1M2U5ZTlkNzJiODI5YzQyMDJmZjU1NDBkZGE3ZDUzIl19LCJjb25maWciOnsiQ21kIjpbImhlbGxvIiwid29ybGQiXX19"
	desc := mf.Config
	if string(desc.Data) != wantConfigData {
		t.Errorf("config: got %q, want %q", string(desc.Data), wantConfigData)
	}
	// Not considering Data, descriptors should be equivalent.
	desc.Data = nil
	if d := cmp.Diff(orig.Config, desc); d != "" {
		t.Errorf("config: descriptor diff (-was,+now): %s", d)
	}
}

// TestInlineData_Stream tests that an image that contains any streaming layers
// will not have data inlined.
func TestInlineData_Stream(t *testing.T) {
	img, err := AppendLayers(empty.Image,
		stream.NewLayer(ioutil.NopCloser(strings.NewReader("streeeeam"))))
	if err != nil {
		t.Fatalf("AppendLayers: %v", err)
	}

	got, err := InlineData(img, 1000)
	if err != nil {
		t.Fatalf("InlineData: %v", err)
	}

	if got != img {
		t.Fatalf("image changed: was %v, got %v", got, img)
	}
}
