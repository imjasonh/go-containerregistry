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

package name

import (
	"path"
	"strings"
	"testing"
)

func TestNewDigestTagStrictValidation(t *testing.T) {
	t.Parallel()

	for _, name := range goodStrictValidationTagDigestNames {
		if _, err := NewDigestTag(name, StrictValidation); err != nil {
			t.Errorf("%q should be a valid DigestTag name, got error: %v", name, err)
		}
	}

	for _, name := range badDigestNames {
		if repo, err := NewDigestTag(name, StrictValidation); err == nil {
			t.Errorf("%q should be an invalid DigestTag name, got Digest: %#v", name, repo)
		}
	}
}

func TestNewDigestTag(t *testing.T) {
	t.Parallel()

	for _, name := range goodWeakValidationTagDigestNames {
		if _, err := NewDigestTag(name, WeakValidation); err != nil {
			t.Errorf("%q should be a valid DigestTag name, got error: %v", name, err)
		}
	}

	for _, name := range badDigestNames {
		if repo, err := NewDigestTag(name, WeakValidation); err == nil {
			t.Errorf("%q should be an invalid DigestTag name, got Digest: %#v", name, repo)
		}
	}
}

func TestDigestTagComponents(t *testing.T) {
	t.Parallel()
	testRegistry := "gcr.io"
	testRepository := "project-id/image"
	fullRepo := path.Join(testRegistry, testRepository)
	tag := "hello"

	digestTagNameStr := testRegistry + "/" + testRepository + ":" + tag + "@" + validDigest
	dt, err := NewDigestTag(digestTagNameStr, StrictValidation)
	if err != nil {
		t.Fatalf("%q should be a valid Digest name, got error: %v", digestTagNameStr, err)
	}

	if got := dt.String(); got != digestTagNameStr {
		t.Errorf("%q String(); want %q got %q", dt, digestTagNameStr, got)
	}
	if got := dt.Identifier(); got != validDigest {
		t.Errorf("%q Identifier() want %q got %q", dt, validDigest, got)
	}
	actualRegistry := dt.RegistryStr()
	if actualRegistry != testRegistry {
		t.Errorf("%q RegistryStr() want %q got %q", dt, testRegistry, actualRegistry)
	}
	actualRepository := dt.RepositoryStr()
	if actualRepository != testRepository {
		t.Errorf("%q RepositoryStr() want %q got %q", dt, testRepository, actualRepository)
	}
	contextRepo := dt.Context().String()
	if contextRepo != fullRepo {
		t.Errorf("%q Context().String() want %q got %q", dt, fullRepo, contextRepo)
	}
	actualDigest := dt.DigestStr()
	if actualDigest != validDigest {
		t.Errorf("%q DigestStr() want %q got %q", dt, validDigest, actualDigest)
	}
	actualTag := dt.TagStr()
	if actualTag != tag {
		t.Errorf("%q TagStr(): want %q got %q", dt, tag, actualTag)
	}
}

func TestDigestTagScopes(t *testing.T) {
	t.Parallel()
	testRegistry := "gcr.io"
	testRepo := "project-id/image"
	testAction := "pull"
	tag := "hello"

	expectedScope := strings.Join([]string{"repository", testRepo, testAction}, ":")

	digestTagNameStr := testRegistry + "/" + testRepo + ":" + tag + "@" + validDigest
	dt, err := NewDigestTag(digestTagNameStr, StrictValidation)
	if err != nil {
		t.Fatalf("%q should be a valid Digest name, got error: %v", digestTagNameStr, err)
	}

	actualScope := dt.Scope(testAction)
	if actualScope != expectedScope {
		t.Errorf("%q scope want %q got %q", dt, expectedScope, actualScope)
	}
}
