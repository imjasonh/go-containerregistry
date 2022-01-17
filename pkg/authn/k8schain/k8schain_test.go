package k8schain

import (
	"context"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

func TestPull(t *testing.T) {
	s := httptest.NewServer(registry.New())
	defer s.Close()
	u, err := url.Parse(s.URL)
	if err != nil {
		t.Fatal(err)
	}

	ref, err := name.ParseReference(u.Host + "/image")
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	kc, err := NewNoClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	img, err := random.Image(100, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(ref, img, remote.WithAuthFromKeychain(kc)); err != nil {
		t.Fatalf("Pushing image: %v", err)
	}
	if _, err := remote.Image(ref, remote.WithAuthFromKeychain(kc)); err != nil {
		t.Fatalf("Pulling image: %v", err)
	}
}
