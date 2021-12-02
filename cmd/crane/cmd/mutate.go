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

package cmd

import (
	"fmt"
	"log"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/spf13/cobra"
)

// NewCmdMutate creates a new cobra.Command for the mutate subcommand.
func NewCmdMutate(options *[]crane.Option) *cobra.Command {
	var labels map[string]string
	var entrypoint []string
	var newRef string
	var annotations map[string]string

	mutateCmd := &cobra.Command{
		Use:   "mutate",
		Short: "Modify image labels and annotations. The container must be pushed to a registry, and the manifest is updated there.",
		Args:  cobra.ExactArgs(1),
		Run: func(_ *cobra.Command, args []string) {
			// Pull image and get config.
			ref := args[0]

			if len(annotations) != 0 {
				desc, err := crane.Head(ref, *options...)
				if err != nil {
					log.Fatalf("checking %s: %v", ref, err)
				}
				if desc.MediaType.IsIndex() {
					log.Fatalf("mutating annotations on an index is not yet supported")
				}
			}

			img, err := crane.Pull(ref, *options...)
			if err != nil {
				log.Fatalf("pulling %s: %v", ref, err)
			}
			cfg, err := img.ConfigFile()
			if err != nil {
				log.Fatalf("getting config: %v", err)
			}
			cfg = cfg.DeepCopy()

			// Set labels.
			if cfg.Config.Labels == nil {
				cfg.Config.Labels = map[string]string{}
			}

			err = validateKeyVals(labels)
			if err != nil {
				log.Fatal(err)
			}

			for k, v := range labels {
				cfg.Config.Labels[k] = v
			}

			err = validateKeyVals(annotations)
			if err != nil {
				log.Fatal(err)
			}

			// Set entrypoint.
			if len(entrypoint) > 0 {
				cfg.Config.Entrypoint = entrypoint
			}

			// Mutate and write image.
			img, err = mutate.Config(img, cfg.Config)
			if err != nil {
				log.Fatalf("mutating config: %v", err)
			}

			img = mutate.Annotations(img, annotations).(v1.Image)

			// If the new ref isn't provided, write over the original image.
			// If that ref was provided by digest (e.g., output from
			// another crane command), then strip that and push the
			// mutated image by digest instead.
			if newRef == "" {
				newRef = ref
			}
			digest, err := img.Digest()
			if err != nil {
				log.Fatalf("digesting new image: %v", err)
			}
			r, err := name.ParseReference(newRef)
			if err != nil {
				log.Fatalf("parsing %s: %v", newRef, err)
			}
			if _, ok := r.(name.Digest); ok {
				newRef = r.Context().Digest(digest.String()).String()
			}
			if err := crane.Push(img, newRef, *options...); err != nil {
				log.Fatalf("pushing %s: %v", newRef, err)
			}
			fmt.Println(r.Context().Digest(digest.String()))
		},
	}
	mutateCmd.Flags().StringToStringVarP(&annotations, "annotation", "a", nil, "New annotations to add")
	mutateCmd.Flags().StringToStringVarP(&labels, "label", "l", nil, "New labels to add")
	mutateCmd.Flags().StringSliceVar(&entrypoint, "entrypoint", nil, "New entrypoint to set")
	mutateCmd.Flags().StringVarP(&newRef, "tag", "t", "", "New tag to apply to mutated image. If not provided, push by digest to the original image repository.")
	return mutateCmd
}

// validateKeyVals ensures no values are empty, returns error if they are
func validateKeyVals(kvPairs map[string]string) error {
	for label, value := range kvPairs {
		if value == "" {
			return fmt.Errorf("parsing label %q, value is empty", label)
		}
	}
	return nil
}
