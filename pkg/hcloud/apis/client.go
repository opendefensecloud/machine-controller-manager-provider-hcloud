/*
Copyright (c) 2021 SAP SE or an SAP affiliate company. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package apis is the main package for provider specific APIs
package apis

import (
	"os"
	"strings"
	"sync"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"k8s.io/klog/v2"

	"github.com/opendefensecloud/machine-controller-manager-provider-hcloud/pkg/version"
)

var (
	singletons   = make(map[string]*hcloud.Client)
	singletonsMu sync.Mutex
)

// GetClientForToken returns an underlying HCloud client for the given token.
//
// PARAMETERS
// token string Token to look up client instance for
func GetClientForToken(token string) *hcloud.Client {
	// if one accidentially copies a newline character into the token, remove it!
	if strings.Contains(token, "\n") {
		klog.InfoS("Your HCloud token contains a newline character. I will remove it for you but you should consider to remove it.")
		token = strings.ReplaceAll(token, "\n", "")
	}

	singletonsMu.Lock()
	defer singletonsMu.Unlock()

	client, ok := singletons[token]

	if !ok {
		opts := []hcloud.ClientOption{
			hcloud.WithToken(token),
			hcloud.WithApplication("machine-controller-manager-provider-hcloud", version.Version),
		}
		if endpoint := os.Getenv("HCLOUD_ENDPOINT"); endpoint != "" {
			opts = append(opts, hcloud.WithEndpoint(endpoint))
		}
		client = hcloud.NewClient(opts...)
		singletons[token] = client
	}

	return client
}

// SetClientForToken sets a preconfigured HCloud client for the given token.
//
// PARAMETERS
// token  string         Token to look up client instance for
// client *hcloud.Client Preconfigured HCloud client
func SetClientForToken(token string, client *hcloud.Client) {
	singletonsMu.Lock()
	defer singletonsMu.Unlock()

	if client == nil {
		delete(singletons, token)
	} else {
		singletons[token] = client
	}
}
