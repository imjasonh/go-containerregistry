package registry

import (
	"context"
	"errors"
	"io"
	"io/ioutil"
	"os"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/match"
)

type layoutHandler struct {
	lp layout.Path
}

func (l layoutHandler) Stat(ctx context.Context, repo string, h v1.Hash) (int64, error) {
	// TODO: layout.StatBlob ?
	rc, err := l.lp.Blob(h)
	if errors.Is(err, os.ErrNotExist) {
		return 0, errNotFound
	}
	defer rc.Close()
	return io.Copy(ioutil.Discard, rc)
}
func (l layoutHandler) Get(_ context.Context, _ string, h v1.Hash) (io.ReadCloser, error) {
	rc, err := l.lp.Blob(h)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errNotFound
	}
	return rc, err
}
func (l layoutHandler) Put(_ context.Context, _ string, h v1.Hash, rc io.ReadCloser) error {
	return l.lp.WriteBlob(h, rc)
}
func (l layoutHandler) StatUpload(_ context.Context, uploadID string) (int64, error) {
	return -1, errNotFound // TODO
}
func (l layoutHandler) AppendUpload(_ context.Context, uploadID string, rc io.ReadCloser) (int64, error) {
	return -1, errNotFound // TODO
}
func (l layoutHandler) FinishUpload(_ context.Context, uploadID string, rc io.ReadCloser) (io.ReadCloser, int64, error) {
	return nil, -1, errNotFound // TODO
}
func (l layoutHandler) FinalizeUpload(_ context.Context, uploadID string, rc io.ReadCloser, h v1.Hash) error {
	return errNotFound // TODO
}
func (l layoutHandler) GetManifestByDigest(ctx context.Context, repo, digest string) (*manifest, error) {
	h, err := v1.NewHash(digest)
	if err != nil {
		return nil, err
	}
	mf := &manifest{}
	img, err := l.lp.Image(h)
	if err == nil {
		mt, err := img.MediaType()
		if err != nil {
			return nil, err
		}
		mf.contentType = string(mt)
		mf.blob, err = img.RawManifest()
		if err != nil {
			return nil, err
		}
	} else if errors.Is(err, os.ErrNotExist) {
		// Check if it's an index.
		idx, err := l.lp.ImageIndex()
		if errors.Is(err, os.ErrNotExist) {
			return nil, errNotFound
		} else if err != nil {
			return nil, err
		}
		mt, err := idx.MediaType()
		if err != nil {
			return nil, err
		}
		mf.contentType = string(mt)
		mf.blob, err = idx.RawManifest()
		if err != nil {
			return nil, err
		}
	} else {
		return nil, err
	}
	return mf, nil
}
func (l layoutHandler) GetManifestByTag(ctx context.Context, repo, tag string) (*manifest, error) {
	return nil, errNotFound // TODO
}
func (l layoutHandler) PutManifest(ctx context.Context, repo, digest string, mf manifest) error {
	return errNotFound // TODO
}
func (l layoutHandler) TagManifest(ctx context.Context, repo, digest, tag string) error {
	return errNotFound // TODO
}
func (l layoutHandler) DeleteManifest(ctx context.Context, repo, digest string) error {
	// TODO: check if it exists first
	h, err := v1.NewHash(digest)
	if err != nil {
		return err
	}
	return l.lp.RemoveDescriptors(match.Digests(h))
}
func (l layoutHandler) DeleteManifestByTag(ctx context.Context, repo, tag string) error {
	return errNotFound // TODO
}
func (l layoutHandler) ListTags(ctx context.Context, repo string, n int) ([]string, error) {
	return nil, errNotFound // TODO
}
func (l layoutHandler) Catalog(_ context.Context, n int) ([]string, error) {
	return nil, errNotFound // TODO
}
