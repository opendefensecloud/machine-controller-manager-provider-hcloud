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

// Package hcloud contains the HCloud provider specific implementations to manage machines
package hcloud

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gardener/machine-controller-manager/pkg/util/provider/driver"
	"github.com/gardener/machine-controller-manager/pkg/util/provider/machinecodes/codes"
	"github.com/gardener/machine-controller-manager/pkg/util/provider/machinecodes/status"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/opendefensecloud/machine-controller-manager-provider-hcloud/pkg/hcloud/apis"
	"github.com/opendefensecloud/machine-controller-manager-provider-hcloud/pkg/hcloud/apis/mock"
)

var provider *MachineProvider

var _ = BeforeSuite(func() {
	provider = &MachineProvider{}
})

var _ = Describe("MachineController", func() {
	var mockTestEnv mock.MockTestEnv
	providerSecret := &corev1.Secret{
		Data: map[string][]byte{
			"token":    []byte("dummy-token"),
			"userData": []byte("dummy-user-data"),
		},
	}

	var _ = BeforeEach(func() {
		mockTestEnv = mock.NewMockTestEnv()

		apis.SetClientForToken("dummy-token", mockTestEnv.Client)
		mock.SetupFloatingIPsEndpointOnMux(mockTestEnv.Mux)
		mock.SetupImagesEndpointOnMux(mockTestEnv.Mux)
		mock.SetupServerTypesEndpointOnMux(mockTestEnv.Mux, "x86")
		mock.SetupServersEndpointOnMux(mockTestEnv.Mux, true)
		mock.SetupSshKeysEndpointOnMux(mockTestEnv.Mux)
		mock.SetupTestPlacementGroupEndpointOnMux(mockTestEnv.Mux)
		mock.SetupTestServerEndpointOnMux(mockTestEnv.Mux)
		mock.ResetPlacementGroupHitCounters()
	})

	var _ = AfterEach(func() {
		mockTestEnv.Teardown()
		apis.SetClientForToken("dummy-token", nil)
	})

	Describe("#CreateMachine", func() {
		type setup struct {
		}

		type action struct {
			machineRequest *driver.CreateMachineRequest
		}

		type expect struct {
			errToHaveOccurred bool
			errStatus         codes.Code
		}

		type data struct {
			setup  setup
			action action
			expect expect
		}

		DescribeTable("##table",
			func(data *data) {
				ctx := context.Background()
				_, err := provider.CreateMachine(ctx, data.action.machineRequest)

				if data.expect.errToHaveOccurred {
					Expect(err).To(HaveOccurred())

					errStatus, ok := err.(*status.Status)
					Expect(ok).To(BeTrue())
					Expect(errStatus.Code()).To(Equal(data.expect.errStatus))
				} else {
					Expect(err).NotTo(HaveOccurred())
				}
			},

			Entry("is correctly executed", &data{
				setup: setup{},
				action: action{
					&driver.CreateMachineRequest{
						Machine:      mock.NewMachine(-1),
						MachineClass: mock.NewMachineClass(),
						Secret:       providerSecret,
					},
				},
				expect: expect{
					errToHaveOccurred: false,
				},
			}),

			Entry("contains a provider ID", &data{
				setup: setup{},
				action: action{
					&driver.CreateMachineRequest{
						Machine:      mock.NewMachine(mock.TestServerID),
						MachineClass: mock.NewMachineClass(),
						Secret:       providerSecret,
					},
				},
				expect: expect{
					errToHaveOccurred: true,
					errStatus:         codes.InvalidArgument,
				},
			}),
			Entry("contains an invalid provider ID", &data{
				setup: setup{},
				action: action{
					&driver.CreateMachineRequest{
						Machine:      mock.ManipulateMachine(mock.NewMachine(mock.TestServerID), map[string]interface{}{"Spec.ProviderID": "test:///invalid"}),
						MachineClass: mock.NewMachineClass(),
						Secret:       providerSecret,
					},
				},
				expect: expect{
					errToHaveOccurred: true,
					errStatus:         codes.InvalidArgument,
				},
			}),
			Entry("contains an invalid machine class", &data{
				setup: setup{},
				action: action{
					&driver.CreateMachineRequest{
						Machine:      mock.NewMachine(-1),
						MachineClass: mock.NewMachineClassWithProviderSpec([]byte(mock.TestInvalidProviderSpec)),
						Secret:       providerSecret,
					},
				},
				expect: expect{
					errToHaveOccurred: true,
					errStatus:         codes.InvalidArgument,
				},
			}),
		)

		It("applies the placement group when PlacementGroupID is set without a floating pool", func() {
			ctx := context.Background()

			// TestProviderSpec carries placementGroupID "42" and no floatingPoolName,
			// so the placement-group flow must be driven by PlacementGroupID alone.
			_, err := provider.CreateMachine(ctx, &driver.CreateMachineRequest{
				Machine:      mock.NewMachine(-1),
				MachineClass: mock.NewMachineClass(),
				Secret:       providerSecret,
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(mock.PlacementGroupGetByIDHitCount()).To(BeNumerically(">", 0), "expected GET /placement_groups/42 to be hit")
			Expect(mock.AddToPlacementGroupHitCount()).To(BeNumerically(">", 0), "expected add_to_placement_group action to be hit")
		})

		It("selects the image variant matching the server type's CPU architecture", func() {
			ctx := context.Background()

			env := mock.NewMockTestEnv()
			defer env.Teardown()

			const token = "arm-image-token"
			apis.SetClientForToken(token, env.Client)
			defer apis.SetClientForToken(token, nil)

			// TestImageName exists for both x86 and arm, x86 listed first. Without filtering
			// by the server type's architecture the x86 image wins and an arm server type is
			// created with an image it cannot boot.
			mock.SetupServerTypesEndpointOnMux(env.Mux, "arm")
			mock.SetupImagesEndpointOnMux(env.Mux)
			mock.SetupServersEndpointOnMux(env.Mux, true)
			mock.SetupSshKeysEndpointOnMux(env.Mux)
			mock.SetupFloatingIPsEndpointOnMux(env.Mux)
			mock.SetupTestPlacementGroupEndpointOnMux(env.Mux)
			mock.SetupTestServerEndpointOnMux(env.Mux)

			secret := &corev1.Secret{
				Data: map[string][]byte{
					"token":    []byte(token),
					"userData": []byte("dummy-user-data"),
				},
			}

			_, err := provider.CreateMachine(ctx, &driver.CreateMachineRequest{
				Machine:      mock.NewMachine(-1),
				MachineClass: mock.NewMachineClass(),
				Secret:       secret,
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(mock.CreatedServerImageID()).To(Equal(int64(mock.TestArmImageID)))
		})

		It("returns InvalidArgument when the server type does not exist", func() {
			ctx := context.Background()

			spec := []byte(`{"cluster":"xyz","zone":"hel1-dc2","imageName":"ubuntu-20.04","serverType":"does-not-exist","sshFingerprint":"00:11:22:33:44:55:66:77:88:99:aa:bb:cc:dd:ee:ff"}`)

			_, err := provider.CreateMachine(ctx, &driver.CreateMachineRequest{
				Machine:      mock.NewMachine(-1),
				MachineClass: mock.NewMachineClassWithProviderSpec(spec),
				Secret:       providerSecret,
			})

			Expect(err).To(HaveOccurred())

			errStatus, ok := err.(*status.Status)
			Expect(ok).To(BeTrue())
			Expect(errStatus.Code()).To(Equal(codes.InvalidArgument))
		})

		It("does not delete a foreign server that merely shares a name (never deletes by name)", func() {
			ctx := context.Background()

			env := mock.NewMockTestEnv()
			defer env.Teardown()

			const token = "cleanup-foreign-token"
			apis.SetClientForToken(token, env.Client)
			defer apis.SetClientForToken(token, nil)

			mock.SetupImagesEndpointOnMux(env.Mux)
			mock.SetupSshKeysEndpointOnMux(env.Mux)
			mock.SetupFloatingIPsEndpointOnMux(env.Mux)
			// emptyOnFirstRequest=false: GetByName finds a server named "machine-0"
			// immediately. That fixture server carries no labels, so it does not match
			// this request's providerSpec labels and createMachineValidateDeploymentAndCleanup
			// correctly treats it as foreign (not created by this request).
			mock.SetupServersEndpointOnMux(env.Mux, false)
			mock.SetupTestServerEndpointOnMux(env.Mux)
			mock.ResetServerDeleteHitCounter()

			secret := &corev1.Secret{
				Data: map[string][]byte{
					"token":    []byte(token),
					"userData": []byte("dummy-user-data"),
				},
			}

			_, err := provider.CreateMachine(ctx, &driver.CreateMachineRequest{
				Machine:      mock.NewMachine(-1),
				MachineClass: mock.NewMachineClass(),
				Secret:       secret,
			})

			Expect(err).To(HaveOccurred())
			Expect(mock.ServerDeleteHitCount()).To(Equal(int32(0)), "create-error cleanup must never delete a server found only by name")
		})

		It("fails create when the initial Server.GetByName lookup errors transiently", func() {
			ctx := context.Background()

			env := mock.NewMockTestEnv()
			defer env.Teardown()

			const token = "getbyname-error-token"
			apis.SetClientForToken(token, env.Client)
			defer apis.SetClientForToken(token, nil)

			// No /images or /ssh_keys handlers registered: if the GetByName error is
			// (incorrectly) swallowed, execution falls through to those lookups and
			// fails there instead -- with the wrong error code. Only a fix that
			// surfaces the GetByName error directly returns codes.Unavailable here.
			env.Mux.HandleFunc("/servers", func(res http.ResponseWriter, req *http.Request) {
				if strings.ToLower(req.Method) == "get" {
					res.WriteHeader(http.StatusInternalServerError)
					return
				}
				panic("unexpected method on /servers in this test")
			})

			secret := &corev1.Secret{
				Data: map[string][]byte{
					"token":    []byte(token),
					"userData": []byte("dummy-user-data"),
				},
			}

			_, err := provider.CreateMachine(ctx, &driver.CreateMachineRequest{
				Machine:      mock.NewMachine(-1),
				MachineClass: mock.NewMachineClass(),
				Secret:       secret,
			})

			Expect(err).To(HaveOccurred())

			errStatus, ok := err.(*status.Status)
			Expect(ok).To(BeTrue())
			Expect(errStatus.Code()).To(Equal(codes.Unavailable))
		})

		It("returns Unavailable when the image lookup fails transiently", func() {
			ctx := context.Background()

			env := mock.NewMockTestEnv()
			defer env.Teardown()

			const token = "image-lookup-error-token"
			apis.SetClientForToken(token, env.Client)
			defer apis.SetClientForToken(token, nil)

			mock.SetupServersEndpointOnMux(env.Mux, true)
			mock.SetupServerTypesEndpointOnMux(env.Mux, "x86")
			env.Mux.HandleFunc("/images", func(res http.ResponseWriter, req *http.Request) {
				res.WriteHeader(http.StatusInternalServerError)
			})

			secret := &corev1.Secret{
				Data: map[string][]byte{
					"token":    []byte(token),
					"userData": []byte("dummy-user-data"),
				},
			}

			_, err := provider.CreateMachine(ctx, &driver.CreateMachineRequest{
				Machine:      mock.NewMachine(-1),
				MachineClass: mock.NewMachineClass(),
				Secret:       secret,
			})

			Expect(err).To(HaveOccurred())

			errStatus, ok := err.(*status.Status)
			Expect(ok).To(BeTrue())
			Expect(errStatus.Code()).To(Equal(codes.Unavailable))
		})

		It("returns Unavailable when the SSH key lookup fails transiently", func() {
			ctx := context.Background()

			env := mock.NewMockTestEnv()
			defer env.Teardown()

			const token = "sshkey-lookup-error-token"
			apis.SetClientForToken(token, env.Client)
			defer apis.SetClientForToken(token, nil)

			mock.SetupServersEndpointOnMux(env.Mux, true)
			mock.SetupServerTypesEndpointOnMux(env.Mux, "x86")
			mock.SetupImagesEndpointOnMux(env.Mux)
			env.Mux.HandleFunc("/ssh_keys", func(res http.ResponseWriter, req *http.Request) {
				res.WriteHeader(http.StatusInternalServerError)
			})

			secret := &corev1.Secret{
				Data: map[string][]byte{
					"token":    []byte(token),
					"userData": []byte("dummy-user-data"),
				},
			}

			_, err := provider.CreateMachine(ctx, &driver.CreateMachineRequest{
				Machine:      mock.NewMachine(-1),
				MachineClass: mock.NewMachineClass(),
				Secret:       secret,
			})

			Expect(err).To(HaveOccurred())

			errStatus, ok := err.(*status.Status)
			Expect(ok).To(BeTrue())
			Expect(errStatus.Code()).To(Equal(codes.Unavailable))
		})

		It("returns Unavailable when the network lookup fails transiently", func() {
			ctx := context.Background()

			env := mock.NewMockTestEnv()
			defer env.Teardown()

			const token = "network-lookup-error-token"
			apis.SetClientForToken(token, env.Client)
			defer apis.SetClientForToken(token, nil)

			mock.SetupServersEndpointOnMux(env.Mux, true)
			mock.SetupServerTypesEndpointOnMux(env.Mux, "x86")
			mock.SetupImagesEndpointOnMux(env.Mux)
			mock.SetupSshKeysEndpointOnMux(env.Mux)
			env.Mux.HandleFunc("/networks", func(res http.ResponseWriter, req *http.Request) {
				res.WriteHeader(http.StatusInternalServerError)
			})

			spec := []byte(`{"cluster":"xyz","zone":"hel1-dc2","imageName":"ubuntu-20.04","serverType":"cx11-ceph","sshFingerprint":"00:11:22:33:44:55:66:77:88:99:aa:bb:cc:dd:ee:ff","networkName":"mynet"}`)

			secret := &corev1.Secret{
				Data: map[string][]byte{
					"token":    []byte(token),
					"userData": []byte("dummy-user-data"),
				},
			}

			_, err := provider.CreateMachine(ctx, &driver.CreateMachineRequest{
				Machine:      mock.NewMachine(-1),
				MachineClass: mock.NewMachineClassWithProviderSpec(spec),
				Secret:       secret,
			})

			Expect(err).To(HaveOccurred())

			errStatus, ok := err.(*status.Status)
			Expect(ok).To(BeTrue())
			Expect(errStatus.Code()).To(Equal(codes.Unavailable))
		})
	})

	Describe("#DeleteMachine", func() {
		type setup struct {
		}

		type action struct {
			machineRequest *driver.DeleteMachineRequest
		}

		type expect struct {
			errToHaveOccurred bool
			errStatus         codes.Code
		}

		type data struct {
			setup  setup
			action action
			expect expect
		}

		DescribeTable("##table",
			func(data *data) {
				ctx := context.Background()
				_, err := provider.DeleteMachine(ctx, data.action.machineRequest)

				if data.expect.errToHaveOccurred {
					Expect(err).To(HaveOccurred())

					errStatus, ok := err.(*status.Status)
					Expect(ok).To(BeTrue())
					Expect(errStatus.Code()).To(Equal(data.expect.errStatus))
				} else {
					Expect(err).NotTo(HaveOccurred())
				}
			},

			Entry("is correctly executed", &data{
				setup: setup{},
				action: action{
					&driver.DeleteMachineRequest{
						Machine:      mock.NewMachine(mock.TestServerID),
						MachineClass: mock.NewMachineClass(),
						Secret:       providerSecret,
					},
				},
				expect: expect{
					errToHaveOccurred: false,
				},
			}),

			Entry("contains no provider ID", &data{
				setup: setup{},
				action: action{
					&driver.DeleteMachineRequest{
						Machine:      mock.NewMachine(-1),
						MachineClass: mock.NewMachineClass(),
						Secret:       providerSecret,
					},
				},
				expect: expect{
					errToHaveOccurred: false,
				},
			}),
			Entry("contains an invalid provider ID", &data{
				setup: setup{},
				action: action{
					&driver.DeleteMachineRequest{
						Machine:      mock.ManipulateMachine(mock.NewMachine(mock.TestServerID), map[string]interface{}{"Spec.ProviderID": "test:///invalid"}),
						MachineClass: mock.NewMachineClass(),
						Secret:       providerSecret,
					},
				},
				expect: expect{
					errToHaveOccurred: true,
					errStatus:         codes.InvalidArgument,
				},
			}),
			Entry("server lookup by ID fails transiently", &data{
				setup: setup{},
				action: action{
					&driver.DeleteMachineRequest{
						// Server ID 9999 has no mock handler registered, so the HTTP
						// request hits an unmatched path -- a genuine transient-style
						// API failure, distinct from a well-formed "not found" response.
						Machine:      mock.ManipulateMachine(mock.NewMachine(mock.TestServerID), map[string]interface{}{"Spec.ProviderID": fmt.Sprintf("hcloud:///%s/9999", mock.TestZone)}),
						MachineClass: mock.NewMachineClass(),
						Secret:       providerSecret,
					},
				},
				expect: expect{
					errToHaveOccurred: true,
					errStatus:         codes.Unavailable,
				},
			}),
		)
	})

	Describe("#GetMachineStatus", func() {
		type setup struct {
		}

		type action struct {
			machineRequest *driver.GetMachineStatusRequest
		}

		type expect struct {
			errToHaveOccurred bool
			errStatus         codes.Code
		}

		type data struct {
			setup  setup
			action action
			expect expect
		}

		DescribeTable("##table",
			func(data *data) {
				ctx := context.Background()
				_, err := provider.GetMachineStatus(ctx, data.action.machineRequest)

				if data.expect.errToHaveOccurred {
					Expect(err).To(HaveOccurred())

					errStatus, ok := err.(*status.Status)
					Expect(ok).To(BeTrue())
					Expect(errStatus.Code()).To(Equal(data.expect.errStatus))
				} else {
					Expect(err).NotTo(HaveOccurred())
				}
			},

			Entry("is correctly executed", &data{
				setup: setup{},
				action: action{
					&driver.GetMachineStatusRequest{
						Machine:      mock.NewMachine(mock.TestServerID),
						MachineClass: mock.NewMachineClass(),
						Secret:       providerSecret,
					},
				},
				expect: expect{
					errToHaveOccurred: false,
				},
			}),

			Entry("contains no provider ID", &data{
				setup: setup{},
				action: action{
					&driver.GetMachineStatusRequest{
						Machine:      mock.NewMachine(-1),
						MachineClass: mock.NewMachineClass(),
						Secret:       providerSecret,
					},
				},
				expect: expect{
					errToHaveOccurred: true,
					errStatus:         codes.NotFound,
				},
			}),
			Entry("contains an invalid provider ID", &data{
				setup: setup{},
				action: action{
					&driver.GetMachineStatusRequest{
						Machine:      mock.ManipulateMachine(mock.NewMachine(mock.TestServerID), map[string]interface{}{"Spec.ProviderID": "test:///invalid"}),
						MachineClass: mock.NewMachineClass(),
						Secret:       providerSecret,
					},
				},
				expect: expect{
					errToHaveOccurred: true,
					errStatus:         codes.InvalidArgument,
				},
			}),
			Entry("server lookup by ID fails transiently", &data{
				setup: setup{},
				action: action{
					&driver.GetMachineStatusRequest{
						// Server ID 9999 has no mock handler registered, so the HTTP
						// request hits an unmatched path -- a genuine transient-style
						// API failure, distinct from a well-formed "not found" response.
						Machine:      mock.ManipulateMachine(mock.NewMachine(mock.TestServerID), map[string]interface{}{"Spec.ProviderID": fmt.Sprintf("hcloud:///%s/9999", mock.TestZone)}),
						MachineClass: mock.NewMachineClass(),
						Secret:       providerSecret,
					},
				},
				expect: expect{
					errToHaveOccurred: true,
					errStatus:         codes.Unavailable,
				},
			}),
		)

		It("returns the real server ID in ProviderID when called with a non-empty providerID", func() {
			ctx := context.Background()

			resp, err := provider.GetMachineStatus(ctx, &driver.GetMachineStatusRequest{
				Machine:      mock.NewMachine(mock.TestServerID),
				MachineClass: mock.NewMachineClass(),
				Secret:       providerSecret,
			})

			Expect(err).NotTo(HaveOccurred())

			expectedProviderID := fmt.Sprintf("hcloud:///%s/%d", mock.TestZone, mock.TestServerID)
			Expect(resp.ProviderID).To(Equal(expectedProviderID))
			Expect(resp.ProviderID).NotTo(HaveSuffix("/0"), "ProviderID must encode the real server ID, not the zero value left by a shadowed serverID")
		})
	})

	Describe("#ListMachines", func() {
		type setup struct {
		}

		type action struct {
			machineRequest *driver.ListMachinesRequest
		}

		type expect struct {
			errToHaveOccurred bool
			errStatus         codes.Code
		}

		type data struct {
			setup  setup
			action action
			expect expect
		}

		DescribeTable("##table",
			func(data *data) {
				ctx := context.Background()
				_, err := provider.ListMachines(ctx, data.action.machineRequest)

				if data.expect.errToHaveOccurred {
					Expect(err).To(HaveOccurred())

					errStatus, ok := err.(*status.Status)
					Expect(ok).To(BeTrue())
					Expect(errStatus.Code()).To(Equal(data.expect.errStatus))
				} else {
					Expect(err).NotTo(HaveOccurred())
				}
			},

			Entry("is correctly executed", &data{
				setup: setup{},
				action: action{
					&driver.ListMachinesRequest{
						MachineClass: mock.NewMachineClass(),
						Secret:       providerSecret,
					},
				},
				expect: expect{
					errToHaveOccurred: false,
				},
			}),
			Entry("contains an invalid machine class", &data{
				setup: setup{},
				action: action{
					&driver.ListMachinesRequest{
						MachineClass: mock.NewMachineClassWithProviderSpec([]byte(mock.TestInvalidProviderSpec)),
						Secret:       providerSecret,
					},
				},
				expect: expect{
					errToHaveOccurred: true,
					errStatus:         codes.InvalidArgument,
				},
			}),
		)
	})

	Describe("#GetVolumeIDs", func() {
		It("is not implemented", func() {
			ctx := context.Background()

			req := &driver.GetVolumeIDsRequest{
				PVSpecs: []*corev1.PersistentVolumeSpec{
					{
						Capacity:                      map[corev1.ResourceName]resource.Quantity{},
						PersistentVolumeSource:        corev1.PersistentVolumeSource{},
						AccessModes:                   []corev1.PersistentVolumeAccessMode{},
						ClaimRef:                      &corev1.ObjectReference{},
						PersistentVolumeReclaimPolicy: "",
						StorageClassName:              "",
						MountOptions:                  []string{},
						NodeAffinity:                  &corev1.VolumeNodeAffinity{},
					},
				},
			}

			_, err := provider.GetVolumeIDs(ctx, req)
			Expect(err).To(HaveOccurred())

			errStatus, ok := err.(*status.Status)
			Expect(ok).To(BeTrue())
			Expect(errStatus.Code()).To(Equal(codes.Unimplemented))
		})
	})
})
