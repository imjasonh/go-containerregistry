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

package registry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

type catalog struct {
	Repos []string `json:"repositories"`
}

type listTags struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

type manifest struct {
	contentType string
	blob        []byte
}

type manifests struct {
	manifestHandler manifestHandler

	log *log.Logger
}

func isManifest(req *http.Request) bool {
	elems := strings.Split(req.URL.Path, "/")
	elems = elems[1:]
	if len(elems) < 4 {
		return false
	}
	return elems[len(elems)-2] == "manifests"
}

func isTags(req *http.Request) bool {
	elems := strings.Split(req.URL.Path, "/")
	elems = elems[1:]
	if len(elems) < 4 {
		return false
	}
	return elems[len(elems)-2] == "tags"
}

func isCatalog(req *http.Request) bool {
	elems := strings.Split(req.URL.Path, "/")
	elems = elems[1:]
	if len(elems) < 2 {
		return false
	}

	return elems[len(elems)-1] == "_catalog"
}

// manifestHandler represents a minimal manifest storage backend, capable of
// serving manifests by digest.
type manifestHandler interface {
	GetManifestByDigest(ctx context.Context, repo, digest string) (*manifest, error)
}

// manifestTagGetHandler is an extension interface representing a manifest
// storage backend that can serve manifests by tag.
type manifestTagGetHandler interface {
	GetManifestByTag(ctx context.Context, repo, tag string) (*manifest, error)
}

// manifestPutHandler is an extension interface representing a manifest storage
// backend that can write manifests by digest.
type manifestPutHandler interface {
	PutManifest(ctx context.Context, repo, digest string, mf manifest) error
}

// manifestTagHandler is an extension interface representing a manifest storage
// backend that can tag manifests stored by digest.
type manifestTagHandler interface {
	TagManifest(ctx context.Context, repo, digest, tag string) error
}

// manifestDeleteHandler is an extension interface representing a manifest
// storage backend that can delete manifests by digest.
type manifestDeleteHandler interface {
	DeleteManifest(ctx context.Context, repo, digest string) error
}

// manifestDeleteTagHandler is an extension interface representing a manifest
// storage backend that can delete manifests by tag.
type manifestDeleteTagHandler interface {
	DeleteManifestByTag(ctx context.Context, repo, tag string) error
}

// manifestTagListHandler is an extension interface representing a manifest
// storage backend that can list tags for a repository.
type manifestTagListHandler interface {
	ListTags(ctx context.Context, repo string, limit int) ([]string, error)
}

// catalogHandler is an extension interface representing a manifest storage
// backend that can list repositories.
type catalogHandler interface {
	Catalog(ctx context.Context, limit int) ([]string, error)
}

func (m *memHandler) GetManifestByDigest(ctx context.Context, repo, digest string) (*manifest, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.manifests == nil {
		return nil, errNotFound
	}
	if m.manifests[repo] == nil {
		return nil, errNotFound
	}
	mf, ok := m.manifests[repo][digest]
	if !ok {
		return nil, errNotFound
	}
	return &mf, nil
}
func (m *memHandler) GetManifestByTag(ctx context.Context, repo, tag string) (*manifest, error) {
	return m.GetManifestByDigest(ctx, repo, tag)
}
func (m *memHandler) PutManifest(ctx context.Context, repo, digest string, mf manifest) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.manifests == nil {
		return errNotFound
	}
	if m.manifests[repo] == nil {
		m.manifests[repo] = map[string]manifest{}
	}
	m.manifests[repo][digest] = mf
	return nil
}
func (m *memHandler) TagManifest(ctx context.Context, repo, digest, tag string) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.manifests == nil {
		return errNotFound
	}
	if m.manifests[repo] == nil {
		return errNotFound
	}
	mf, ok := m.manifests[repo][digest]
	if !ok {
		return errNotFound
	}
	m.manifests[repo][tag] = mf
	return nil
}
func (m *memHandler) DeleteManifest(ctx context.Context, repo, digest string) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.manifests == nil {
		return errNotFound
	}
	if m.manifests[repo] == nil {
		return errNotFound
	}
	if _, ok := m.manifests[repo][digest]; !ok {
		return errNotFound
	}
	delete(m.manifests[repo], digest)
	return nil
}
func (m *memHandler) DeleteManifestByTag(ctx context.Context, repo, tag string) error {
	return m.DeleteManifest(ctx, repo, tag)
}
func (m *memHandler) ListTags(ctx context.Context, repo string, n int) ([]string, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	var tags []string
	c, ok := m.manifests[repo]
	if !ok {
		return nil, errNotFound
	}
	count := 0
	for tag := range c {
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
func (m *memHandler) Catalog(_ context.Context, n int) ([]string, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	var repos []string
	count := 0
	for key := range m.manifests {
		if count >= n {
			break
		}
		count++
		repos = append(repos, key)
	}
	sort.Strings(repos)
	return repos, nil
}

// https://github.com/opencontainers/distribution-spec/blob/master/spec.md#pulling-an-image-manifest
// https://github.com/opencontainers/distribution-spec/blob/master/spec.md#pushing-an-image
func (m *manifests) handle(resp http.ResponseWriter, req *http.Request) *regError {
	ctx := req.Context()
	elem := strings.Split(req.URL.Path, "/")
	elem = elem[1:]
	target := elem[len(elem)-1]
	repo := strings.Join(elem[1:len(elem)-2], "/")

	switch req.Method {
	case http.MethodGet:
		var mf *manifest
		var err error
		if strings.HasPrefix(target, "sha256:") {
			mf, err = m.manifestHandler.GetManifestByDigest(ctx, repo, target)
		} else {
			mth, ok := m.manifestHandler.(manifestTagGetHandler)
			if !ok {
				return regErrUnsupported
			}
			mf, err = mth.GetManifestByTag(ctx, repo, target)
		}
		if err == errNotFound {
			return regErrManifestUnknown
		}
		rd := sha256.Sum256(mf.blob)
		d := "sha256:" + hex.EncodeToString(rd[:])
		resp.Header().Set("Docker-Content-Digest", d)
		resp.Header().Set("Content-Type", mf.contentType)
		resp.Header().Set("Content-Length", fmt.Sprint(len(mf.blob)))
		resp.WriteHeader(http.StatusOK)
		io.Copy(resp, bytes.NewReader(mf.blob))
		return nil

	case http.MethodHead:
		var mf *manifest
		var err error
		if strings.HasPrefix(target, "sha256:") {
			mf, err = m.manifestHandler.GetManifestByDigest(ctx, repo, target)
		} else {
			mth, ok := m.manifestHandler.(manifestTagGetHandler)
			if !ok {
				return regErrUnsupported
			}
			mf, err = mth.GetManifestByTag(ctx, repo, target)
		}
		if err == errNotFound {
			return regErrManifestUnknown
		}
		rd := sha256.Sum256(mf.blob)
		d := "sha256:" + hex.EncodeToString(rd[:])
		resp.Header().Set("Docker-Content-Digest", d)
		resp.Header().Set("Content-Type", mf.contentType)
		resp.Header().Set("Content-Length", fmt.Sprint(len(mf.blob)))
		resp.WriteHeader(http.StatusOK)
		return nil

	case http.MethodPut:
		mph, ok := m.manifestHandler.(manifestPutHandler)
		if !ok {
			return regErrUnsupported
		}

		b := &bytes.Buffer{}
		io.Copy(b, req.Body)
		rd := sha256.Sum256(b.Bytes())
		digest := "sha256:" + hex.EncodeToString(rd[:])
		mf := manifest{
			blob:        b.Bytes(),
			contentType: req.Header.Get("Content-Type"),
		}
		if err := mph.PutManifest(ctx, repo, digest, mf); err == errNotFound {
			return regErrManifestUnknown
		} else if err != nil {
			return regErrInternal(err)
		}

		// If the manifest is a manifest list, check that the manifest
		// list's constituent manifests are already uploaded.
		// This isn't strictly required by the registry API, but some
		// registries require this.
		if types.MediaType(mf.contentType).IsIndex() {
			im, err := v1.ParseIndexManifest(b)
			if err != nil {
				return &regError{
					Status:  http.StatusBadRequest,
					Code:    "MANIFEST_INVALID",
					Message: err.Error(),
				}
			}
			for _, desc := range im.Manifests {
				if !desc.MediaType.IsDistributable() {
					continue
				}
				if desc.MediaType.IsIndex() || desc.MediaType.IsImage() {
					if _, err := m.manifestHandler.GetManifestByDigest(ctx, repo, desc.Digest.String()); err == errNotFound {
						return &regError{
							Status:  http.StatusNotFound,
							Code:    "MANIFEST_UNKNOWN",
							Message: fmt.Sprintf("Sub-manifest %q not found", desc.Digest),
						}
					} else if err != nil {
						return regErrInternal(err)
					}
				} else {
					// TODO: Probably want to do an existence check for blobs.
					m.log.Printf("TODO: Check blobs for %q", desc.Digest)
				}
			}
		}

		// Allow future references by target (tag).
		// See https://docs.docker.com/engine/reference/commandline/pull/#pull-an-image-by-digest-immutable-identifier.
		if mth, ok := m.manifestHandler.(manifestTagHandler); ok {
			if err := mth.TagManifest(ctx, repo, digest, target); err == errNotFound {
				return regErrManifestUnknown
			} else if err != nil {
				return regErrInternal(err)
			}
		}

		resp.Header().Set("Docker-Content-Digest", digest)
		resp.WriteHeader(http.StatusCreated)
		return nil

	case http.MethodDelete:
		mdh, ok := m.manifestHandler.(manifestDeleteHandler)
		if !ok {
			return regErrUnsupported
		}
		if err := mdh.DeleteManifest(ctx, repo, target); err == errNotFound {
			return regErrManifestUnknown
		} else if err != nil {
			return regErrInternal(err)
		}
		resp.WriteHeader(http.StatusAccepted)
		return nil

	default:
		return &regError{
			Status:  http.StatusBadRequest,
			Code:    "METHOD_UNKNOWN",
			Message: "We don't understand your method + url",
		}
	}
}

func (m *manifests) handleTags(resp http.ResponseWriter, req *http.Request) *regError {
	ctx := req.Context()
	elem := strings.Split(req.URL.Path, "/")
	elem = elem[1:]
	repo := strings.Join(elem[1:len(elem)-2], "/")
	query := req.URL.Query()
	nStr := query.Get("n")
	n := 1000
	if nStr != "" {
		n, _ = strconv.Atoi(nStr)
	}

	if req.Method == "GET" {
		mlh, ok := m.manifestHandler.(manifestTagListHandler)
		if !ok {
			return regErrUnsupported
		}
		// TODO: implement pagination https://github.com/opencontainers/distribution-spec/blob/b505e9cc53ec499edbd9c1be32298388921bb705/detail.md#tags-paginated
		tags, err := mlh.ListTags(ctx, repo, n)
		if err == errNotFound {
			return regErrUnknownName
		} else if err != nil {
			return regErrInternal(err)
		}

		tagsToList := listTags{
			Name: repo,
			Tags: tags,
		}

		msg, _ := json.Marshal(tagsToList)
		resp.Header().Set("Content-Length", fmt.Sprint(len(msg)))
		resp.WriteHeader(http.StatusOK)
		io.Copy(resp, bytes.NewReader([]byte(msg)))
		return nil
	}

	return &regError{
		Status:  http.StatusBadRequest,
		Code:    "METHOD_UNKNOWN",
		Message: "We don't understand your method + url",
	}
}

func (m *manifests) handleCatalog(resp http.ResponseWriter, req *http.Request) *regError {
	ctx := req.Context()
	query := req.URL.Query()
	nStr := query.Get("n")
	n := 10000
	if nStr != "" {
		n, _ = strconv.Atoi(nStr)
	}

	if req.Method == "GET" {
		ch, ok := m.manifestHandler.(catalogHandler)
		if !ok {
			return regErrUnsupported
		}

		// TODO: implement pagination
		repos, err := ch.Catalog(ctx, n)
		if err != nil {
			return regErrInternal(err)
		}
		repositoriesToList := catalog{
			Repos: repos,
		}

		msg, _ := json.Marshal(repositoriesToList)
		resp.Header().Set("Content-Length", fmt.Sprint(len(msg)))
		resp.WriteHeader(http.StatusOK)
		io.Copy(resp, bytes.NewReader([]byte(msg)))
		return nil
	}

	return &regError{
		Status:  http.StatusBadRequest,
		Code:    "METHOD_UNKNOWN",
		Message: "We don't understand your method + url",
	}
}
