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

package azure

import (
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/chrismellard/docker-credential-acr-env/pkg/registry"
	"github.com/chrismellard/docker-credential-acr-env/pkg/token"
	"github.com/google/go-containerregistry/pkg/authn"
)

const tokenUsername = "<token>"

// Keychain exports an instance of the azure Keychain.
var Keychain authn.Keychain = azureKeychain{}

type azureKeychain struct{}

// Resolve implements authn.Keychain a la docker-credential-acr-env.
func (azureKeychain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	spToken, settings, err := token.GetServicePrincipalTokenFromEnvironment()
	if err != nil {
		return authn.Anonymous, nil
	}
	refreshToken, err := registry.GetRegistryRefreshTokenFromAADExchange(target.String(), spToken, settings.Values[auth.TenantID])
	if err != nil {
		return authn.Anonymous, nil
	}

	return &authn.Basic{
		Username: tokenUsername,
		Password: refreshToken,
	}, nil
}
