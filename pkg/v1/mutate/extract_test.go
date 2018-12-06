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

package mutate

import (
	"archive/tar"
	"bytes"
	"io"
	"io/ioutil"
	"strings"
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

func TestExtractFile(t *testing.T) {
	img := mustImage(t, map[string]string{
		"present": "hello",
		"deleted": "goodbye",
	}, []string{"deleted"})

	for _, c := range []struct {
		path, want string
		wantErr    error
	}{{
		path: "present",
		want: "hello",
	}, {
		path:    "not-present",
		wantErr: ErrNotExist,
	}, {
		path:    "deleted",
		wantErr: ErrNotExist,
	}} {
		t.Run(c.path, func(t *testing.T) {
			rc, err := ExtractFile(img, c.path)
			if err != c.wantErr {
				t.Errorf("ExtractFile got %v, want %v", err, c.wantErr)
			}
			if err == nil {
				all, err := ioutil.ReadAll(rc)
				if err != nil {
					t.Fatalf("ReadAll: %v", err)
				}
				if string(all) != c.want {
					t.Errorf("ExtractFile got %q, want %q", string(all), c.want)
				}
			}
		})
	}
}

func mustImage(t *testing.T, files map[string]string, deleted []string) v1.Image {
	base := empty.Image
	var err error
	for k, v := range files {
		base, err = AppendLayers(base, mustLayer(t, k, v))
		if err != nil {
			t.Fatalf("AppendLayer: %v", err)
		}
	}
	for _, path := range deleted {
		base, err = AppendLayers(base, mustLayer(t, ".wh."+path, ""))
		if err != nil {
			t.Fatalf("AppendLayer: %v", err)
		}
	}
	return base
}

func mustLayer(t *testing.T, path, contents string) v1.Layer {
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	if err := w.WriteHeader(&tar.Header{
		Name:     path,
		Size:     int64(len(contents)),
		Typeflag: tar.TypeRegA,
	}); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if _, err := io.Copy(w, strings.NewReader(contents)); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	l, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) { return ioutil.NopCloser(&buf), nil })
	if err != nil {
		t.Fatalf("tarball.LayerFromOpener: %v", err)
	}
	return l
}
