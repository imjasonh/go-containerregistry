// Copyright 2023 Google LLC All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package test

import (
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/random"
)

// test.T(t).Digest(img)
//
// or
//
// must := must.T(t)
// must.Digest(img)

// func TestFoo(t *testing.T) {
//   must := test.T(t)
//   img := must.RandomImage(1024, 5)
//   must.Digest(img)
//   must.ParseReference("gcr.io/foo/bar")
//   must.Tag("gcr.io/foo/bar:baz")
// }

func T(t *testing.T) Muster {
	return testMuster{t}
}

type digester interface {
	Digest() (v1.Hash, error)
}

type Muster interface {
	RandomImage(int64, int64) v1.Image
	RandomIndex(int64, int64, int64) v1.ImageIndex
	Digest(digester) v1.Hash
	ParseReference(string) name.Reference
	Tag(string) name.Tag
}

type testMuster struct {
	t *testing.T
}

func (m testMuster) RandomIndex(size, layers, count int64) v1.ImageIndex {
	m.t.Helper()
	idx, err := random.Index(size, layers, count)
	if err != nil {
		m.t.Fatalf("random.Index(): %v", err)
	}
	return idx
}

func (m testMuster) RandomImage(size, layers int64) v1.Image {
	m.t.Helper()
	img, err := random.Image(size, layers)
	if err != nil {
		m.t.Fatalf("random.Image(): %v", err)
	}
	return img
}

func (m testMuster) Digest(i digester) v1.Hash {
	m.t.Helper()
	h, err := i.Digest()
	if err != nil {
		m.t.Fatalf("Digest(): %v", err)
	}
	return h
}

func (m testMuster) ParseReference(s string) name.Reference {
	m.t.Helper()
	r, err := name.ParseReference(s)
	if err != nil {
		m.t.Fatalf("ParseReference(%q): %v", s, err)
	}
	return r
}

func (m testMuster) Tag(s string) name.Tag {
	m.t.Helper()
	r, err := name.NewTag(s)
	if err != nil {
		m.t.Fatalf("NewTag(%q): %v", s, err)
	}
	return r
}

// Must returns a GenericMuster[T] that can be used to test functions that must return T, or otherwise will t.Fatal.
func Must[T any](t *testing.T) GenericMuster[T] {
	return GenericMuster[T]{t}
}

type GenericMuster[T any] struct {
	t *testing.T
}

func (g GenericMuster[T]) Do(t T, err error) T {
	g.t.Helper()
	if err != nil {
		g.t.Fatalf("Do(): %v", err)
	}
	return t
}
