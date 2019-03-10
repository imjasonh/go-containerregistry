// Copyright 2021 Google LLC All Rights Reserved.
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
package explore

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/go-containerregistry/internal/gzip"
	"github.com/google/go-containerregistry/internal/verify"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// We should not buffer blobs greater than 1MB
const tooBig = 2 << 20

type handler struct {
	mux    *http.ServeMux
	remote []remote.Option
}

type Option func(h *handler)

func WithRemote(opt []remote.Option) Option {
	return func(h *handler) {
		h.remote = opt
	}
}

func New(opts ...Option) http.Handler {
	h := handler{
		mux: http.NewServeMux(),
	}

	for _, opt := range opts {
		opt(&h)
	}

	h.mux.HandleFunc("/", h.root)
	h.mux.HandleFunc("/fs/", h.fsHandler)

	// Janky workaround for downloading via the "urls" field.
	h.mux.HandleFunc("/http/", h.fsHandler)
	h.mux.HandleFunc("/https/", h.fsHandler)

	// Just ungzips and dumps the bytes.
	// Useful for looking at something with the wrong mediaType.
	h.mux.HandleFunc("/gzip/", h.fsHandler)

	return &h
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("%v", r.URL)

	h.mux.ServeHTTP(w, r)
}

// Like http.Handler, but with error handling.
func (h *handler) root(w http.ResponseWriter, r *http.Request) {
	if err := h.renderResponse(w, r); err != nil {
		fmt.Fprintf(w, "failed: %v", err)
	}
}

// Like http.Handler, but with error handling.
func (h *handler) fsHandler(w http.ResponseWriter, r *http.Request) {
	if err := h.renderBlob(w, r); err != nil {
		fmt.Fprintf(w, "failed: %v", err)
	}
}

func (h *handler) renderResponse(w http.ResponseWriter, r *http.Request) error {
	qs := r.URL.Query()

	if images, ok := qs["image"]; ok {
		return h.renderManifest(w, r, images[0])
	}
	if blob, ok := getBlobQuery(r); ok {
		return h.renderBlobJSON(w, blob)
	}
	if repos, ok := qs["repo"]; ok {
		return h.renderRepo(w, r, repos[0])
	}

	// Fall back to a helpful landing page.
	return renderLanding(w)
}

func renderLanding(w http.ResponseWriter) error {
	_, err := io.Copy(w, strings.NewReader(landingPage))
	return err
}

// Render repo with tags linking to images.
func (h *handler) renderRepo(w http.ResponseWriter, r *http.Request, repo string) error {
	ref, err := name.NewRepository(repo)
	if err != nil {
		return err
	}

	tags, err := remote.List(ref, h.remote...)
	if err != nil {
		return err
	}

	data := RepositoryData{
		Name: ref.String(),
		Tags: tags,
	}

	return repoTmpl.Execute(w, data)
}

// Render manifests with links to blobs, manifests, etc.
func (h *handler) renderManifest(w http.ResponseWriter, r *http.Request, image string) error {
	qs := r.URL.Query()

	ref, err := name.ParseReference(image, name.WeakValidation)
	if err != nil {
		return err
	}
	d, err := remote.Head(ref, h.remote...)
	if err != nil {
		return err
	}
	if d.Size > tooBig {
		return fmt.Errorf("manifest %s too big: %d", ref, d.Size)
	}
	desc, err := remote.Get(ref, h.remote...)
	if err != nil {
		return err
	}

	data := HeaderData{
		Repo:       ref.Context().String(),
		Image:      ref.String(),
		Reference:  ref,
		Descriptor: desc,
	}

	if _, ok := qs["discovery"]; ok {
		cosignRef, err := munge(ref.Context().Digest(desc.Digest.String()))
		if err != nil {
			return err
		}
		if _, err := remote.Head(cosignRef, h.remote...); err != nil {
			log.Printf("remote.Head(%q): %v", cosignRef.String(), err)
		} else {
			data.CosignTag = cosignRef.Identifier()
		}
	}

	fmt.Fprintf(w, header)

	if err := bodyTmpl.Execute(w, data); err != nil {
		return err
	}
	output := &simpleOutputter{
		w:     w,
		fresh: []bool{},
		repo:  ref.Context().String(),
	}
	if err := renderJSON(output, desc.Manifest); err != nil {
		return err
	}
	// TODO: This is janky.
	output.undiv()

	fmt.Fprintf(w, footer)

	return nil
}

// Render blob as JSON, possibly containing refs to images.
func (h *handler) renderBlobJSON(w http.ResponseWriter, blobRef string) error {
	ref, err := name.NewDigest(blobRef, name.StrictValidation)
	if err != nil {
		return err
	}

	l, err := remote.Layer(ref, h.remote...)
	if err != nil {
		return err
	}
	size, err := l.Size()
	if err != nil {
		return fmt.Errorf("layer %s size: %v", ref, err)
	}
	if size > tooBig {
		return fmt.Errorf("layer %s too big: %d", ref, size)
	}
	blob, err := l.Compressed()
	if err != nil {
		return err
	}
	defer blob.Close()

	fmt.Fprintf(w, header)

	output := &simpleOutputter{
		w:     w,
		fresh: []bool{},
		repo:  ref.Context().String(),
	}

	// TODO: Can we do this in a streaming way?
	b, err := ioutil.ReadAll(blob)
	if err != nil {
		return err
	}
	if err := renderJSON(output, b); err != nil {
		return err
	}
	// TODO: This is janky.
	output.undiv()

	fmt.Fprintf(w, footer)

	return nil
}

// Render blob, either as just ungzipped bytes, or via http.FileServer.
func (h *handler) renderBlob(w http.ResponseWriter, r *http.Request) error {
	log.Printf("%v", r.URL)

	// Bit of a hack for tekton bundles...
	if strings.HasPrefix(r.URL.Path, "/gzip/") {
		blob, _, err := h.fetchBlob(r)
		if err != nil {
			return err
		}
		zr, err := gzip.UnzipReadCloser(blob)
		if err != nil {
			return err
		}
		defer zr.Close()

		_, err = io.Copy(w, zr)
		return err
	}

	fs, err := h.newLayerFS(r)
	if err != nil {
		return err
	}
	defer fs.Close()

	http.FileServer(fs).ServeHTTP(w, r)

	return nil
}

// Fetch blob from registry or URL.
func (h *handler) fetchBlob(r *http.Request) (io.ReadCloser, string, error) {
	path, root, err := splitFsURL(r.URL.Path)
	if err != nil {
		return nil, "", err
	}

	chunks := strings.Split(path, "@")
	if len(chunks) != 2 {
		return nil, "", fmt.Errorf("not enough chunks: %s", path)
	}
	// 71 = len("sha256:") + 64
	if len(chunks[1]) < 71 {
		return nil, "", fmt.Errorf("second chunk too short: %s", chunks[1])
	}

	digest := chunks[1][:71]

	ref := strings.Join([]string{chunks[0], digest}, "@")
	if ref == "" {
		return nil, "", fmt.Errorf("bad ref: %s", path)
	}

	if root == "/http/" || root == "/https/" {
		log.Printf("chunks[0]: %v", chunks[0])

		u, err := url.PathUnescape(chunks[0])
		if err != nil {
			return nil, "", err
		}

		scheme := "https://"
		if root == "/http/" {
			scheme = "http://"
		}
		u = scheme + u
		log.Printf("GET %v", u)

		resp, err := http.Get(u)
		if err != nil {
			return nil, "", err
		}
		if resp.StatusCode == http.StatusOK {
			h, err := v1.NewHash(digest)
			if err != nil {
				return nil, "", err
			}
			checked, err := verify.ReadCloser(resp.Body, h)
			if err != nil {
				return nil, "", err
			}
			return checked, root + ref, nil
		}
		resp.Body.Close()
		log.Printf("GET %s failed: %s", u, resp.Status)
	}

	blobRef, err := name.NewDigest(ref, name.StrictValidation)
	if err != nil {
		return nil, "", err
	}

	l, err := remote.Layer(blobRef, h.remote...)
	if err != nil {
		return nil, "", err
	}
	rc, err := l.Compressed()
	if err != nil {
		return nil, "", err
	}
	return rc, root + ref, err
}

func getBlobQuery(r *http.Request) (string, bool) {
	qs := r.URL.Query()
	if q, ok := qs["config"]; ok {
		return q[0], ok
	}
	if q, ok := qs["cosign"]; ok {
		return q[0], ok
	}
	if q, ok := qs["descriptor"]; ok {
		return q[0], ok
	}

	return "", false
}

func munge(ref name.Reference) (name.Reference, error) {
	munged := strings.ReplaceAll(ref.String(), "@sha256:", "@sha256-")
	munged = strings.ReplaceAll(munged, "@", ":")
	munged = munged + ".cosign"
	return name.ParseReference(munged)
}

func splitFsURL(p string) (string, string, error) {
	for _, prefix := range []string{"/fs/", "/https/", "/http/", "/gzip/"} {
		if strings.HasPrefix(p, prefix) {
			return strings.TrimPrefix(p, prefix), prefix, nil
		}
	}

	return "", "", fmt.Errorf("unexpected path: %v", p)
}
