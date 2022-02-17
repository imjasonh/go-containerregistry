package mutate

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/stream"
)

// InlineData modifies an image's layer descriptors and config descriptor to
// inline the data using the Data field, if the size of the descriptor is below
// the threshold.
//
// If the image contains any streaming layers, no data will be inlined.
func InlineData(img v1.Image, threshold int64) (v1.Image, error) {
	mf, err := img.Manifest()
	if errors.Is(err, stream.ErrNotComputed) {
		return img, nil
	} else if err != nil {
		return nil, err
	}
	mf = mf.DeepCopy()
	inline := func(desc *v1.Descriptor) error {
		l, err := img.LayerByDigest(desc.Digest)
		if err != nil {
			return err
		}
		rc, err := l.Compressed()
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		enc := base64.NewEncoder(base64.StdEncoding, &buf)
		if _, err := io.CopyN(enc, rc, desc.Size); err != nil {
			return err
		}
		desc.Data = buf.Bytes()
		return nil
	}

	for i, desc := range mf.Layers {
		if desc.Size < threshold {
			if err := inline(&desc); err != nil {
				return nil, err
			}
			mf.Layers[i] = desc
		}
	}

	desc := mf.Config
	if desc.Size < threshold {
		if err := inline(&desc); err != nil {
			return nil, err
		}
		mf.Config = desc
	}

	configFile, err := img.ConfigFile()
	if err != nil {
		return nil, err
	}

	return &image{
		base:       img,
		manifest:   mf,
		configFile: configFile,
		computed:   true,
	}, nil
}
