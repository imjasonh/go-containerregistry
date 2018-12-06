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
	"errors"
	"io"
	"log"

	v1 "github.com/google/go-containerregistry/pkg/v1"
)

var ErrNotExist = errors.New("path does not exist")

func ExtractFile(img v1.Image, path string) (io.ReadCloser, error) {
	layers, err := img.Layers()
	if err != nil {
		return nil, err
	}

	for _, l := range layers {
		rc, err := l.Uncompressed()
		if err != nil {
			return nil, err
		}
		tr := tar.NewReader(rc)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			} else if err != nil {
				return nil, err
			}
			log.Println("file:", h.Name)
			if h.Name == path {
				return rc, nil
			}
			if h.Name == ".wh."+path {
				return nil, ErrNotExist
			}
		}
		if err := rc.Close(); err != nil {
			return nil, err
		}
	}
	return nil, ErrNotExist
}
