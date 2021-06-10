// Copyright 2018 Google LLC All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v1

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestGoodManifestSimple(t *testing.T) {
	got, err := ParseManifest(strings.NewReader(`{}`))
	if err != nil {
		t.Errorf("Unexpected error parsing manifest: %v", err)
	}

	want := Manifest{}
	if diff := cmp.Diff(want, *got); diff != "" {
		t.Errorf("ParseManifest({}); (-want +got) %s", diff)
	}
}

func TestGoodManifestWithHash(t *testing.T) {
	good, err := ParseManifest(strings.NewReader(`{
  "config": {
    "digest": "sha256:deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
  }
}`))
	if err != nil {
		t.Errorf("Unexpected error parsing manifest: %v", err)
	}

	if got, want := good.Config.Digest.Algorithm, "sha256"; got != want {
		t.Errorf("ParseManifest().Config.Digest.Algorithm; got %v, want %v", got, want)
	}
}

func TestManifestWithBadHash(t *testing.T) {
	bad, err := ParseManifest(strings.NewReader(`{
  "config": {
    "digest": "sha256:deadbeed"
  }
}`))
	if err == nil {
		t.Errorf("Expected error parsing manifest, but got: %v", bad)
	}
}

func TestParseIndexManifest(t *testing.T) {
	got, err := ParseIndexManifest(strings.NewReader(`{}`))
	if err != nil {
		t.Errorf("Unexpected error parsing manifest: %v", err)
	}

	want := IndexManifest{}
	if diff := cmp.Diff(want, *got); diff != "" {
		t.Errorf("ParseIndexManifest({}); (-want +got) %s", diff)
	}

	if got, err := ParseIndexManifest(strings.NewReader("{")); err == nil {
		t.Errorf("expected error, got: %v", got)
	}
}

func TestDescriptorData(t *testing.T) {
	// generate a little random data.
	data := make([]byte, 11)
	rand.Read(data)

	size := int64(len(data))
	enc := base64.StdEncoding.EncodeToString(data)
	h, _, _ := SHA256(bytes.NewReader(data))

	for _, c := range []struct {
		desc    string
		d       Descriptor
		want    []byte
		wantErr error
	}{{
		desc:    "no data",
		d:       Descriptor{},
		wantErr: ErrNoData,
	}, {
		desc: "good",
		d: Descriptor{
			Digest: h,
			Size:   size,
			Data:   enc,
		},
		want: data,
	}, {
		desc: "bad size (too large)",
		d: Descriptor{
			Digest: h,
			Size:   size + 10,
			Data:   enc,
		},
		wantErr: fmt.Errorf("mismatched size; got 11, want 21"),
	}, {
		desc: "bad size (too small)",
		d: Descriptor{
			Digest: h,
			Size:   size - 5,
			Data:   enc,
		},
		wantErr: fmt.Errorf("mismatched size; got 11, want 6"),
	}, {
		desc: "bad digest",
		d: Descriptor{
			Digest: Hash{
				Algorithm: h.Algorithm,
				Hex:       h.Hex + "haha",
			},
			Size: size,
			Data: enc,
		},
		wantErr: fmt.Errorf("mismatched digest; got %q, want %q", h.Hex, h.Hex+"haha"),
	}} {
		t.Run(c.desc, func(t *testing.T) {
			got, err := c.d.GetData()
			if (err == nil) != (c.wantErr == nil) {
				t.Fatalf("unexpected error: got %v, want %v", err, c.wantErr)
			}
			if err != nil && c.wantErr != nil && err.Error() != c.wantErr.Error() {
				t.Fatalf("unexpected error:\n got %v\nwant %v", err, c.wantErr)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %q, want %q", string(got), string(c.want))
			}
		})
	}
}
