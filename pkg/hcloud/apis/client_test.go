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

package apis

import (
	"sync"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Client", func() {
	Describe("#GetClientForToken", func() {
		It("reuses the client created for a token", func() {
			const token = "reuse-token"
			defer SetClientForToken(token, nil)

			Expect(GetClientForToken(token)).To(BeIdenticalTo(GetClientForToken(token)))
		})

		It("treats a token with a stray newline as the same token", func() {
			const token = "newline-token"
			defer SetClientForToken(token, nil)

			Expect(GetClientForToken(token + "\n")).To(BeIdenticalTo(GetClientForToken(token)))
		})

		It("returns a client injected via SetClientForToken", func() {
			const token = "injected-token"
			defer SetClientForToken(token, nil)

			injected := hcloud.NewClient()
			SetClientForToken(token, injected)

			Expect(GetClientForToken(token)).To(BeIdenticalTo(injected))
		})

		It("is safe for concurrent use", func() {
			const token = "concurrent-token"
			defer SetClientForToken(token, nil)

			var wg sync.WaitGroup
			clients := make([]*hcloud.Client, 16)
			for i := range clients {
				wg.Go(func() {
					clients[i] = GetClientForToken(token)
				})
			}
			wg.Wait()

			for _, client := range clients {
				Expect(client).To(BeIdenticalTo(clients[0]))
			}
		})
	})
})
