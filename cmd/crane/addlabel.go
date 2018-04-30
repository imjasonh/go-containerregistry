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

package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/google/go-containerregistry/authn"
	"github.com/google/go-containerregistry/name"
	"github.com/google/go-containerregistry/v1/empty"
	"github.com/google/go-containerregistry/v1/mutate"
	"github.com/google/go-containerregistry/v1/remote"
	"github.com/spf13/cobra"
)

func init() {
	var newTag string
	addLabelCmd := &cobra.Command{
		Use:   "add-label",
		Short: "Append contents of a tarball to a remote image",
		Args:  cobra.ExactArgs(3),
		Run: func(_ *cobra.Command, args []string) {
			src, key, val := args[0], args[1], args[2]
			addLabel(src, key, val, newTag)
		},
	}
	addLabelCmd.Flags().StringVarP(&newTag, "tag", "o", "", "Tag to apply to new image. If not specified, tag over the specified image.")
	rootCmd.AddCommand(addLabelCmd)
}

func addLabel(src, key, val, newTag string) {
	ref, err := name.ParseReference(src, name.WeakValidation)
	if err != nil {
		log.Fatalln(err)
	}
	auth, err := authn.DefaultKeychain.Resolve(ref.Context().Registry)
	if err != nil {
		log.Fatalln(err)
	}
	i, err := remote.Image(ref, auth, http.DefaultTransport)
	if err != nil {
		log.Fatalln(err)
	}

	if _, ok := ref.(name.Digest); ok && newTag == "" {
		log.Fatalln("Must provide --new_tag when specifying original image by digest")
	}

	cf, err := i.ConfigFile()
	if err != nil {
		log.Fatalln(err)
	}

	newCfg := *cf.Config.DeepCopy()
	if newCfg.Labels == nil {
		newCfg.Labels = map[string]string{}
	}
	newCfg.Labels[key] = val

	// Make a new image based on the original.
	newImg, err := mutate.Config(empty.Image, newCfg)
	if err != nil {
		log.Fatalln(err)
	}
	layers, err := i.Layers()
	if err != nil {
		log.Fatalln(err)
	}
	newImg, err = mutate.AppendLayers(newImg, layers...)
	if err != nil {
		log.Fatalln(err)
	}

	// Unless --new_tag is specified, tag over the original input image.
	newRef := ref
	if newTag != "" {
		newRef, err = name.NewTag(newTag, name.WeakValidation)
		if err != nil {
			log.Fatalln(err)
		}
	}

	if err := remote.Write(newRef, newImg, auth, http.DefaultTransport, remote.WriteOptions{
		MountPaths: []name.Repository{ref.Context()},
	}); err != nil {
		log.Fatalln(err)
	}

	// Print the newly pushed image's digest.
	dig, err := newImg.Digest()
	if err != nil {
		log.Fatalln(err)
	}
	fmt.Print(dig.String())
}
