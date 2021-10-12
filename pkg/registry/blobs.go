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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"path"
	"strings"
	"sync"
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

// blobs
type blobs struct {
	storage Storage
	lock    sync.Mutex
}

func (b *blobs) handle(resp http.ResponseWriter, req *http.Request) *regError {
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

	if req.Method == "HEAD" {
		b.lock.Lock()
		defer b.lock.Unlock()
		b, err := b.storage.GetBlob(target)
		if err != nil {
			return &regError{
				Status:  http.StatusNotFound,
				Code:    "BLOB_UNKNOWN",
				Message: "Unknown blob",
			}
		}

		resp.Header().Set("Content-Length", fmt.Sprint(len(b)))
		resp.Header().Set("Docker-Content-Digest", target)
		resp.WriteHeader(http.StatusOK)
		return nil
	}

	if req.Method == "GET" {
		b.lock.Lock()
		defer b.lock.Unlock()
		b, err := b.storage.GetBlob(target)
		if err != nil {
			return &regError{
				Status:  http.StatusNotFound,
				Code:    "BLOB_UNKNOWN",
				Message: "Unknown blob",
			}
		}

		resp.Header().Set("Content-Length", fmt.Sprint(len(b)))
		resp.Header().Set("Docker-Content-Digest", target)
		resp.WriteHeader(http.StatusOK)
		io.Copy(resp, bytes.NewReader(b))
		return nil
	}

	if req.Method == "POST" && target == "uploads" && digest != "" {
		l := &bytes.Buffer{}
		io.Copy(l, req.Body)
		rd := sha256.Sum256(l.Bytes())
		d := "sha256:" + hex.EncodeToString(rd[:])
		if d != digest {
			return &regError{
				Status:  http.StatusBadRequest,
				Code:    "DIGEST_INVALID",
				Message: "digest does not match contents",
			}
		}

		b.lock.Lock()
		defer b.lock.Unlock()
		if err := b.storage.PutBlob(d, l.Bytes()); err != nil {
			return &regError{
				Status:  http.StatusInternalServerError,
				Code:    "INTERNAL_ERROR",
				Message: err.Error(),
			}
		}
		resp.Header().Set("Docker-Content-Digest", d)
		resp.WriteHeader(http.StatusCreated)
		return nil
	}

	if req.Method == "POST" && target == "uploads" && digest == "" {
		id := fmt.Sprint(rand.Int63())
		resp.Header().Set("Location", "/"+path.Join("v2", path.Join(elem[1:len(elem)-2]...), "blobs/uploads", id))
		resp.Header().Set("Range", "0-0")
		resp.WriteHeader(http.StatusAccepted)
		return nil
	}

	if req.Method == "PATCH" && service == "uploads" && contentRange != "" {
		start, end := 0, 0
		if _, err := fmt.Sscanf(contentRange, "%d-%d", &start, &end); err != nil {
			return &regError{
				Status:  http.StatusRequestedRangeNotSatisfiable,
				Code:    "BLOB_UPLOAD_UNKNOWN",
				Message: "We don't understand your Content-Range",
			}
		}
		b.lock.Lock()
		defer b.lock.Unlock()
		sz, err := b.storage.UploadSize(target)
		if err != nil {
			return &regError{
				Status:  http.StatusInternalServerError,
				Code:    "INTERNAL_ERROR",
				Message: err.Error(),
			}
		}
		if start != sz {
			return &regError{
				Status:  http.StatusRequestedRangeNotSatisfiable,
				Code:    "BLOB_UPLOAD_UNKNOWN",
				Message: "Your content range doesn't match what we have",
			}
		}
		var buf bytes.Buffer
		io.Copy(&buf, req.Body)
		if err := b.storage.AppendUpload(target, buf.Bytes()); err != nil {
			return &regError{
				Status:  http.StatusInternalServerError,
				Code:    "INTERNAL_ERROR",
				Message: err.Error(),
			}
		}
		resp.Header().Set("Location", "/"+path.Join("v2", path.Join(elem[1:len(elem)-3]...), "blobs/uploads", target))
		resp.Header().Set("Range", fmt.Sprintf("0-%d", len(buf.Bytes())-1))
		resp.WriteHeader(http.StatusNoContent)
		return nil
	}

	if req.Method == "PATCH" && service == "uploads" && contentRange == "" {
		b.lock.Lock()
		defer b.lock.Unlock()
		if sz, err := b.storage.UploadSize(target); err == nil && sz > 0 {
			return &regError{
				Status:  http.StatusBadRequest,
				Code:    "BLOB_UPLOAD_INVALID",
				Message: "Stream uploads after first write are not allowed",
			}
		}

		var buf bytes.Buffer
		io.Copy(&buf, req.Body)
		if err := b.storage.AppendUpload(target, buf.Bytes()); err != nil {
			return &regError{
				Status:  http.StatusInternalServerError,
				Code:    "INTERNAL_ERROR",
				Message: err.Error(),
			}
		}

		resp.Header().Set("Location", "/"+path.Join("v2", path.Join(elem[1:len(elem)-3]...), "blobs/uploads", target))
		resp.Header().Set("Range", fmt.Sprintf("0-%d", len(buf.Bytes())-1))
		resp.WriteHeader(http.StatusNoContent)
		return nil
	}

	if req.Method == "PUT" && service == "uploads" && digest == "" {
		return &regError{
			Status:  http.StatusBadRequest,
			Code:    "DIGEST_INVALID",
			Message: "digest not specified",
		}
	}

	if req.Method == "PUT" && service == "uploads" && digest != "" {
		b.lock.Lock()
		defer b.lock.Unlock()

		var buf bytes.Buffer
		io.Copy(&buf, req.Body)
		if err := b.storage.AppendUpload(target, buf.Bytes()); err != nil {
			return &regError{
				Status:  http.StatusInternalServerError,
				Code:    "INTERNAL_ERROR",
				Message: err.Error(),
			}
		}
		rd := sha256.Sum256(buf.Bytes())
		d := "sha256:" + hex.EncodeToString(rd[:])
		if d != digest {
			return &regError{
				Status:  http.StatusBadRequest,
				Code:    "DIGEST_INVALID",
				Message: "digest does not match contents",
			}
		}

		if err := b.storage.PutBlob(d, buf.Bytes()); err != nil {
			return &regError{
				Status:  http.StatusInternalServerError,
				Code:    "INTERNAL_ERROR",
				Message: err.Error(),
			}
		}
		if err := b.storage.DeleteUpload(target); err != nil {
			return &regError{
				Status:  http.StatusInternalServerError,
				Code:    "INTERNAL_ERROR",
				Message: err.Error(),
			}
		}
		resp.Header().Set("Docker-Content-Digest", d)
		resp.WriteHeader(http.StatusCreated)
		return nil
	}

	return &regError{
		Status:  http.StatusBadRequest,
		Code:    "METHOD_UNKNOWN",
		Message: "We don't understand your method + url",
	}
}
