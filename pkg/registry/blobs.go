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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/google/go-containerregistry/internal/verify"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

// Returns whether this url should be handled by the blob handler
// This is complicated because blob is indicated by the trailing path, not the leading path.
// https://github.com/opencontainers/distribution-spec/blob/master/spec.md#pulling-a-layer
// https://github.com/opencontainers/distribution-spec/blob/master/spec.md#pushing-a-layer
func isBlob(req *http.Request) bool {
	elem := strings.Split(req.URL.Path, "/")
	elem = elem[1:]
	if elem[len(elem)-1] == "" {
		elem = elem[:len(elem)-1]
	}
	if len(elem) < 3 {
		return false
	}
	return elem[len(elem)-2] == "blobs" || (elem[len(elem)-3] == "blobs" &&
		elem[len(elem)-2] == "uploads")
}

// blobHandler represents a minimal blob storage backend, capable of serving
// blob contents.
type blobHandler interface {
	// Get gets the blob contents, or errNotFound if the blob wasn't found.
	Get(ctx context.Context, repo string, h v1.Hash) (io.ReadCloser, error)
}

// blobStatHandler is an extension interface representing a blob storage
// backend that can serve metadata about blobs.
type blobStatHandler interface {
	// Stat returns the size of the blob, or errNotFound if the blob wasn't
	// found, or redirectError if the blob can be found elsewhere.
	Stat(ctx context.Context, repo string, h v1.Hash) (int64, error)
}

// blobPutHandler is an extension interface representing a blob storage backend
// that can write blob contents.
type blobPutHandler interface {
	// Put puts the blob contents.
	//
	// The contents will be verified against the expected size and digest
	// as the contents are read, and an error will be returned if these
	// don't match. Implementations should return that error, or a wrapper
	// around that error, to return the correct error when these don't match.
	Put(ctx context.Context, repo string, h v1.Hash, rc io.ReadCloser) error
}

// uploadHandler represents a minimal upload storage backend, capable of
// appending streamed upload contents and finally returning them to be stored
// in blob storage.
type uploadHandler interface {
	// StatUpload returns the current size of the given upload.
	StatUpload(ctx context.Context, uploadID string) (int64, error)

	// AppendUpload appends the contents of the ReadCloser to the current
	// upload contents, and returns the new total size.
	AppendUpload(ctx context.Context, uploadID string, rc io.ReadCloser) (int64, error)

	// FinishUpload appends the contents of the ReadCloser to the curent
	// upload contents, and returns the total contents and their size.
	FinishUpload(ctx context.Context, uploadID string, rc io.ReadCloser) (io.ReadCloser, int64, error)
}

// uploadFinalizeHandler is an extension interface representing upload storage
// that can finalize contents into blob storage.
type uploadFinalizeHandler interface {
	// FinalizeUpload appends the contents of the ReadCloser to the current
	// uplaod contents, checks the total contents digest matches, and
	// finalizes it into blob storage.
	//
	// Implementations that implement this method are responsible for
	// verifying the given hash matches the total contents before
	// persisting to blob storage.
	FinalizeUpload(ctx context.Context, uploadID string, rc io.ReadCloser, h v1.Hash) error
}

// redirectError represents a signal that the blob handler doesn't have the blob
// contents, but that those contents are at another location which registry
// clients should redirect to.
type redirectError struct {
	// Location is the location to find the contents.
	Location string

	// Code is the HTTP redirect status code to return to clients.
	Code int
}

func (e redirectError) Error() string { return fmt.Sprintf("redirecting (%d): %s", e.Code, e.Location) }

// errNotFound represents an error locating the blob.
var errNotFound = errors.New("not found")

type memHandler struct {
	blobs, uploads map[string][]byte
	manifests      map[string]map[string]manifest
	lock           sync.Mutex
}

func (m *memHandler) Stat(_ context.Context, _ string, h v1.Hash) (int64, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	b, found := m.blobs[h.String()]
	if !found {
		return 0, errNotFound
	}
	return int64(len(b)), nil
}
func (m *memHandler) Get(_ context.Context, _ string, h v1.Hash) (io.ReadCloser, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	b, found := m.blobs[h.String()]
	if !found {
		return nil, errNotFound
	}
	return ioutil.NopCloser(bytes.NewReader(b)), nil
}
func (m *memHandler) Put(_ context.Context, _ string, h v1.Hash, rc io.ReadCloser) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	defer rc.Close()
	all, err := ioutil.ReadAll(rc)
	if err != nil {
		return err
	}
	m.blobs[h.String()] = all
	return nil
}
func (m *memHandler) StatUpload(_ context.Context, uploadID string) (int64, error) {
	m.lock.Lock()
	defer m.lock.Unlock()
	return int64(len(m.uploads[uploadID])), nil
}
func (m *memHandler) AppendUpload(_ context.Context, uploadID string, rc io.ReadCloser) (int64, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	defer rc.Close()

	have := m.uploads[uploadID]
	next, err := ioutil.ReadAll(rc)
	if err != nil {
		return -1, err
	}
	all := append(have, next...)
	size := int64(len(all))
	m.uploads[uploadID] = all
	return size, nil
}
func (m *memHandler) FinishUpload(_ context.Context, uploadID string, rc io.ReadCloser) (io.ReadCloser, int64, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	defer rc.Close()

	have := m.uploads[uploadID]
	all, err := ioutil.ReadAll(rc)
	if err != nil {
		return nil, -1, err
	}
	delete(m.uploads, uploadID)
	return ioutil.NopCloser(bytes.NewReader(append(have, all...))), int64(len(have) + len(all)), nil
}
func (m *memHandler) FinalizeUpload(_ context.Context, uploadID string, rc io.ReadCloser, h v1.Hash) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	defer rc.Close()

	have := m.uploads[uploadID]
	last, err := ioutil.ReadAll(rc)
	if err != nil {
		return err
	}
	delete(m.uploads, uploadID)
	all := append(have, last...)
	size := int64(len(all))

	// Read and verify the full contents' digest.
	rc = ioutil.NopCloser(bytes.NewReader(all))
	vrc, err := verify.ReadCloser(rc, size, h)
	if err != nil {
		return err
	}
	defer vrc.Close()
	all, err = ioutil.ReadAll(vrc)
	if err != nil {
		return err
	}

	m.blobs[h.String()] = all
	return nil
}

// blobs
type blobs struct {
	blobHandler blobHandler
}

func (b *blobs) handle(resp http.ResponseWriter, req *http.Request) *regError {
	ctx := req.Context()
	elem := strings.Split(req.URL.Path, "/")
	elem = elem[1:]
	if elem[len(elem)-1] == "" {
		elem = elem[:len(elem)-1]
	}
	// Must have a path of form /v2/{name}/blobs/{upload,sha256:}
	if len(elem) < 4 {
		return &regError{
			Status:  http.StatusBadRequest,
			Code:    "NAME_INVALID",
			Message: "blobs must be attached to a repo",
		}
	}
	target := elem[len(elem)-1]
	service := elem[len(elem)-2]
	digest := req.URL.Query().Get("digest")
	contentRange := req.Header.Get("Content-Range")

	repo := req.URL.Host + path.Join(elem[1:len(elem)-2]...)

	switch req.Method {
	case http.MethodHead:
		h, err := v1.NewHash(target)
		if err != nil {
			return &regError{
				Status:  http.StatusBadRequest,
				Code:    "NAME_INVALID",
				Message: "invalid digest",
			}
		}

		var size int64
		if bsh, ok := b.blobHandler.(blobStatHandler); ok {
			size, err = bsh.Stat(ctx, repo, h)
			if errors.Is(err, errNotFound) {
				return regErrBlobUnknown
			} else if err != nil {
				var rerr redirectError
				if errors.As(err, &rerr) {
					http.Redirect(resp, req, rerr.Location, rerr.Code)
					return nil
				}
				return regErrInternal(err)
			}
		} else {
			rc, err := b.blobHandler.Get(ctx, repo, h)
			if errors.Is(err, errNotFound) {
				return regErrBlobUnknown
			} else if err != nil {
				var rerr redirectError
				if errors.As(err, &rerr) {
					http.Redirect(resp, req, rerr.Location, rerr.Code)
					return nil
				}
				return regErrInternal(err)
			}
			defer rc.Close()
			size, err = io.Copy(ioutil.Discard, rc)
			if err != nil {
				return regErrInternal(err)
			}
		}

		resp.Header().Set("Content-Length", fmt.Sprint(size))
		resp.Header().Set("Docker-Content-Digest", h.String())
		resp.WriteHeader(http.StatusOK)
		return nil

	case http.MethodGet:
		h, err := v1.NewHash(target)
		if err != nil {
			return &regError{
				Status:  http.StatusBadRequest,
				Code:    "NAME_INVALID",
				Message: "invalid digest",
			}
		}

		var size int64
		var r io.Reader
		if bsh, ok := b.blobHandler.(blobStatHandler); ok {
			size, err = bsh.Stat(ctx, repo, h)
			if errors.Is(err, errNotFound) {
				return regErrBlobUnknown
			} else if err != nil {
				var rerr redirectError
				if errors.As(err, &rerr) {
					http.Redirect(resp, req, rerr.Location, rerr.Code)
					return nil
				}
				return regErrInternal(err)
			}

			rc, err := b.blobHandler.Get(ctx, repo, h)
			if errors.Is(err, errNotFound) {
				return regErrBlobUnknown
			} else if err != nil {
				var rerr redirectError
				if errors.As(err, &rerr) {
					http.Redirect(resp, req, rerr.Location, rerr.Code)
					return nil
				}

				return regErrInternal(err)
			}
			defer rc.Close()
			r = rc
		} else {
			tmp, err := b.blobHandler.Get(ctx, repo, h)
			if errors.Is(err, errNotFound) {
				return regErrBlobUnknown
			} else if err != nil {
				var rerr redirectError
				if errors.As(err, &rerr) {
					http.Redirect(resp, req, rerr.Location, rerr.Code)
					return nil
				}

				return regErrInternal(err)
			}
			defer tmp.Close()
			var buf bytes.Buffer
			io.Copy(&buf, tmp)
			size = int64(buf.Len())
			r = &buf
		}

		resp.Header().Set("Content-Length", fmt.Sprint(size))
		resp.Header().Set("Docker-Content-Digest", h.String())
		resp.WriteHeader(http.StatusOK)
		io.Copy(resp, r)
		return nil

	case http.MethodPost:
		bph, ok := b.blobHandler.(blobPutHandler)
		if !ok {
			return regErrUnsupported
		}

		// It is weird that this is "target" instead of "service", but
		// that's how the index math works out above.
		if target != "uploads" {
			return &regError{
				Status:  http.StatusBadRequest,
				Code:    "METHOD_UNKNOWN",
				Message: fmt.Sprintf("POST to /blobs must be followed by /uploads, got %s", target),
			}
		}

		if digest != "" {
			h, err := v1.NewHash(digest)
			if err != nil {
				return regErrDigestInvalid
			}

			vrc, err := verify.ReadCloser(req.Body, req.ContentLength, h)
			if err != nil {
				return regErrInternal(err)
			}
			defer vrc.Close()

			if err = bph.Put(req.Context(), repo, h, vrc); err != nil {
				if errors.As(err, &verify.Error{}) {
					log.Printf("Digest mismatch: %v", err)
					return regErrDigestMismatch
				}
				return regErrInternal(err)
			}
			resp.Header().Set("Docker-Content-Digest", h.String())
			resp.WriteHeader(http.StatusCreated)
			return nil
		}

		if _, ok := b.blobHandler.(uploadHandler); !ok {
			// Registry doesn't support streamed uploads, only
			// monolithic blob PUTs.
			return regErrUnsupported
		}

		id := fmt.Sprint(rand.Int63())
		resp.Header().Set("Location", "/"+path.Join("v2", path.Join(elem[1:len(elem)-2]...), "blobs/uploads", id))
		resp.Header().Set("Range", "0-0")
		resp.WriteHeader(http.StatusAccepted)
		return nil

	case http.MethodPatch:
		if service != "uploads" {
			return &regError{
				Status:  http.StatusBadRequest,
				Code:    "METHOD_UNKNOWN",
				Message: fmt.Sprintf("PATCH to /blobs must be followed by /uploads, got %s", service),
			}
		}
		uh, ok := b.blobHandler.(uploadHandler)
		if !ok {
			return regErrUnsupported
		}

		if contentRange != "" {
			var start int64
			if _, err := fmt.Sscanf(contentRange, "%d-%d", &start, new(int)); err != nil {
				return &regError{
					Status:  http.StatusRequestedRangeNotSatisfiable,
					Code:    "BLOB_UPLOAD_UNKNOWN",
					Message: "We don't understand your Content-Range",
				}
			}

			size, err := uh.StatUpload(ctx, target)
			if err != nil {
				return regErrInternal(err)
			}
			if start != size {
				return &regError{
					Status:  http.StatusRequestedRangeNotSatisfiable,
					Code:    "BLOB_UPLOAD_UNKNOWN",
					Message: "Your content range doesn't match what we have",
				}
			}

			size, err = uh.AppendUpload(ctx, target, req.Body)
			if err != nil {
				return regErrInternal(err)
			}

			resp.Header().Set("Location", "/"+path.Join("v2", path.Join(elem[1:len(elem)-3]...), "blobs/uploads", target))
			resp.Header().Set("Range", fmt.Sprintf("0-%d", size-1))
			resp.WriteHeader(http.StatusNoContent)
			return nil
		}

		if size, err := uh.StatUpload(ctx, target); err != nil {
			return regErrInternal(err)
		} else if size != 0 {
			return &regError{
				Status:  http.StatusBadRequest,
				Code:    "BLOB_UPLOAD_INVALID",
				Message: "Stream uploads after first write are not allowed",
			}
		}

		size, err := uh.AppendUpload(ctx, target, req.Body)
		if err != nil {
			return regErrInternal(err)
		}
		resp.Header().Set("Location", "/"+path.Join("v2", path.Join(elem[1:len(elem)-3]...), "blobs/uploads", target))
		resp.Header().Set("Range", fmt.Sprintf("0-%d", size-1))
		resp.WriteHeader(http.StatusNoContent)
		return nil

	case http.MethodPut:
		uh, ok := b.blobHandler.(uploadHandler)
		if !ok {
			return regErrUnsupported
		}

		if service != "uploads" {
			return &regError{
				Status:  http.StatusBadRequest,
				Code:    "METHOD_UNKNOWN",
				Message: fmt.Sprintf("PUT to /blobs must be followed by /uploads, got %s", service),
			}
		}

		if digest == "" {
			return &regError{
				Status:  http.StatusBadRequest,
				Code:    "DIGEST_INVALID",
				Message: "digest not specified",
			}
		}

		h, err := v1.NewHash(digest)
		if err != nil {
			return &regError{
				Status:  http.StatusBadRequest,
				Code:    "NAME_INVALID",
				Message: "invalid digest",
			}
		}

		if ufh, ok := uh.(uploadFinalizeHandler); ok {
			if err := ufh.FinalizeUpload(ctx, target, req.Body, h); err != nil {
				return regErrInternal(err)
			}
			resp.Header().Set("Docker-Content-Digest", h.String())
			resp.WriteHeader(http.StatusCreated)
			return nil
		}

		bph, ok := b.blobHandler.(blobPutHandler)
		if !ok {
			return regErrUnsupported
		}

		rc, size, err := uh.FinishUpload(ctx, target, req.Body)
		if err != nil {
			return regErrInternal(err)
		}

		vrc, err := verify.ReadCloser(rc, size, h)
		if err != nil {
			return regErrInternal(err)
		}
		defer vrc.Close()

		if err := bph.Put(ctx, repo, h, vrc); err != nil {
			if errors.As(err, &verify.Error{}) {
				log.Printf("Digest mismatch: %v", err)
				return regErrDigestMismatch
			}
			return regErrInternal(err)
		}

		resp.Header().Set("Docker-Content-Digest", h.String())
		resp.WriteHeader(http.StatusCreated)
		return nil

	default:
		return &regError{
			Status:  http.StatusBadRequest,
			Code:    "METHOD_UNKNOWN",
			Message: "We don't understand your method + url",
		}
	}
}
