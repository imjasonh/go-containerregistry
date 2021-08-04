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
	"errors"
	"strings"
)

// DigestTag stores a tag-and-digest name in a structured form.
type DigestTag struct {
	Repository
	digest, tag string
	original    string
}

// Ensure DigestTag implements Reference
var _ Reference = (*DigestTag)(nil)

// Context implements Reference.
func (d DigestTag) Context() Repository {
	return d.Repository
}

// Identifier implements Reference.
func (d DigestTag) Identifier() string {
	return d.DigestStr()
}

// DigestStr returns the digest component of the DigestTag.
func (d DigestTag) DigestStr() string {
	return d.digest
}

// TagStr returns the tag component of the Tag.
func (d DigestTag) TagStr() string {
	return d.tag
}

// Name returns the name from which the DigestTag was derived.
func (d DigestTag) Name() string {
	return d.Repository.Name() + tagDelim + d.TagStr() + digestDelim + d.DigestStr()
}

// String returns the original input string.
func (d DigestTag) String() string {
	return d.original
}

// AsDigest returns the DigestTag as a Digest, losing information about the tag.
func (d DigestTag) AsDigest() Digest {
	return Digest{
		Repository: d.Repository,
		digest:     d.digest,
		original:   d.original,
	}
}

// AsTag returns the DigestTag as a Tag, losing information about the digest.
func (d DigestTag) AsTag() Tag {
	return Tag{
		Repository: d.Repository,
		tag:        d.tag,
		original:   d.original,
	}
}

// NewDigestTag returns a new DigestTag representing the given name.
func NewDigestTag(name string, opts ...Option) (DigestTag, error) {
	// Split on "@"
	parts := strings.Split(name, digestDelim)
	if len(parts) != 2 {
		return DigestTag{}, NewErrBadName("a digest must contain exactly one '@' separator (e.g. registry/repository@digest) saw: %s", name)
	}
	digest := parts[1]

	// Always check that the digest is valid.
	if err := checkDigest(digest); err != nil {
		return DigestTag{}, err
	}

	// Split on ":"
	var tag string
	parts = strings.Split(parts[0], tagDelim)
	base := parts[0]
	// Verify that we aren't confusing a tag for a hostname w/ port for the purposes of weak validation.
	if len(parts) > 1 && !strings.Contains(parts[len(parts)-1], regRepoDelimiter) {
		base = strings.Join(parts[:len(parts)-1], tagDelim)
		tag = parts[len(parts)-1]
	}
	if tag == "" {
		return DigestTag{}, errors.New("tag is required")
	}

	repo, err := NewRepository(base, opts...)
	if err != nil {
		return DigestTag{}, err
	}
	return DigestTag{
		Repository: repo,
		digest:     digest,
		tag:        tag,
		original:   name,
	}, nil
}
