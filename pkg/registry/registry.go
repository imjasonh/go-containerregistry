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

// Package registry implements a docker V2 registry and the OCI distribution specification.
//
// It is designed to be used anywhere a low dependency container registry is needed, with an
// initial focus on tests.
//
// Its goal is to be standards compliant and its strictness will increase over time.
//
// This is currently a low flightmiles system. It's likely quite safe to use in tests; If you're using it
// in production, please let us know how and send us CL's for integration tests.
package registry

import (
	"errors"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
)

type registry struct {
	log       *log.Logger
	storage   Storage
	blobs     blobs
	manifests manifests
}

type Storage interface {
	PutBlob(string, []byte) error
	GetBlob(string) ([]byte, error)

	AppendUpload(string, []byte) error
	DeleteUpload(string) error
	UploadSize(string) (int, error)

	PutManifest(repo, target string, m manifest) error
	GetManifest(repo, target string) (manifest, error)
	DeleteManifest(repo, target string) error
	ListTags(repo string, n int) ([]string, error)
	Catalog(n int) ([]string, error)
}

// https://docs.docker.com/registry/spec/api/#api-version-check
// https://github.com/opencontainers/distribution-spec/blob/master/spec.md#api-version-check
func (r *registry) v2(resp http.ResponseWriter, req *http.Request) *regError {
	if isBlob(req) {
		return r.blobs.handle(resp, req)
	}
	if isManifest(req) {
		return r.manifests.handle(resp, req)
	}
	if isTags(req) {
		return r.manifests.handleTags(resp, req)
	}
	if isCatalog(req) {
		return r.manifests.handleCatalog(resp, req)
	}
	resp.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
	if req.URL.Path != "/v2/" && req.URL.Path != "/v2" {
		return &regError{
			Status:  http.StatusNotFound,
			Code:    "METHOD_UNKNOWN",
			Message: "We don't understand your method + url",
		}
	}
	resp.WriteHeader(200)
	return nil
}

func (r *registry) root(resp http.ResponseWriter, req *http.Request) {
	if rerr := r.v2(resp, req); rerr != nil {
		r.log.Printf("%s %s %d %s %s", req.Method, req.URL, rerr.Status, rerr.Code, rerr.Message)
		rerr.Write(resp)
		return
	}
	r.log.Printf("%s %s", req.Method, req.URL)
}

// New returns a handler which implements the docker registry protocol.
// It should be registered at the site root.
func New(opts ...Option) http.Handler {
	r := &registry{
		log:   log.New(os.Stderr, "", log.LstdFlags),
		blobs: blobs{},
		manifests: manifests{
			log: log.New(os.Stderr, "", log.LstdFlags),
		},
	}
	r.storage = &memStore{
		blobs:     map[string][]byte{},
		uploads:   map[string][]byte{},
		manifests: map[string]map[string]manifest{},
	}
	for _, o := range opts {
		o(r)
	}
	r.blobs.storage = r.storage
	r.manifests.storage = r.storage
	return http.HandlerFunc(r.root)
}

// Option describes the available options
// for creating the registry.
type Option func(r *registry)

// Logger overrides the logger used to record requests to the registry.
func Logger(l *log.Logger) Option {
	return func(r *registry) {
		r.log = l
		r.manifests.log = l
	}
}

// WithStorage overrides the default in-memory storage.
func WithStorage(s Storage) Option {
	return func(r *registry) {
		r.storage = s
	}
}

var ErrNotFound = errors.New("not found")

type memStore struct {
	blobs, uploads map[string][]byte
	manifests      map[string]map[string]manifest
}

func (m *memStore) PutBlob(s string, b []byte) error {
	m.blobs[s] = b
	return nil
}

func (m *memStore) GetBlob(s string) ([]byte, error) {
	b, ok := m.blobs[s]
	if !ok {
		return nil, ErrNotFound
	}
	return b, nil
}

func (m *memStore) AppendUpload(s string, b []byte) error {
	m.uploads[s] = append(m.uploads[s], b...)
	return nil
}

func (m *memStore) DeleteUpload(s string) error {
	delete(m.uploads, s)
	return nil
}

func (m *memStore) UploadSize(s string) (int, error) {
	return len(m.uploads[s]), nil
}

func (m *memStore) PutManifest(repo, target string, mf manifest) error {
	if _, ok := m.manifests[repo]; !ok {
		m.manifests[repo] = map[string]manifest{}
	}
	m.manifests[repo][target] = mf
	return nil
}

func (m *memStore) GetManifest(repo, target string) (manifest, error) {
	if _, ok := m.manifests[repo]; !ok {
		m.manifests[repo] = map[string]manifest{}
	}
	mf, ok := m.manifests[repo][target]
	if !ok {
		return manifest{}, ErrNotFound
	}
	return mf, nil
}

func (m *memStore) DeleteManifest(repo, target string) error {
	if _, ok := m.manifests[repo]; !ok {
		m.manifests[repo] = map[string]manifest{}
	}
	if _, ok := m.manifests[repo][target]; !ok {
		return ErrNotFound
	}
	delete(m.manifests[repo], target)
	return nil
}

func (m *memStore) ListTags(repo string, n int) ([]string, error) {
	if _, ok := m.manifests[repo]; !ok {
		return nil, ErrNotFound
	}
	var tags []string
	count := 0
	for tag := range m.manifests[repo] {
		if count >= n {
			break
		}
		count++
		if !strings.Contains(tag, "sha256:") {
			tags = append(tags, tag)
		}
	}
	sort.Strings(tags)
	return tags, nil
}

func (m *memStore) Catalog(n int) ([]string, error) {
	var repos []string
	count := 0
	// TODO: implement pagination
	for key := range m.manifests {
		if count >= n {
			break
		}
		count++

		repos = append(repos, key)
	}
	return repos, nil
}
