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

// Package mock provides all methods required to simulate a driver
package mock

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/gardener/machine-controller-manager/pkg/apis/machine/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// placementGroupGetByIDHits and addToPlacementGroupHits count how many times the
// respective mock endpoints have been invoked. Tests use these counters to assert
// that the placement-group flow was actually exercised, not merely that CreateMachine
// returned without error.
var (
	placementGroupGetByIDHits int32
	addToPlacementGroupHits   int32
)

// ResetPlacementGroupHitCounters resets the placement-group hit counters. Call this
// from BeforeEach so counts don't leak between test cases.
func ResetPlacementGroupHitCounters() {
	atomic.StoreInt32(&placementGroupGetByIDHits, 0)
	atomic.StoreInt32(&addToPlacementGroupHits, 0)
}

// PlacementGroupGetByIDHitCount returns how many times GET /placement_groups/{id} was hit.
func PlacementGroupGetByIDHitCount() int32 {
	return atomic.LoadInt32(&placementGroupGetByIDHits)
}

// AddToPlacementGroupHitCount returns how many times POST .../actions/add_to_placement_group was hit.
func AddToPlacementGroupHitCount() int32 {
	return atomic.LoadInt32(&addToPlacementGroupHits)
}

// serverDeleteHits counts how many times DELETE /servers/{id} was hit via the endpoint
// registered by SetupTestServerEndpointOnMux. Tests use this to prove that create-error
// cleanup never deletes a server it did not itself create.
var serverDeleteHits int32

// ResetServerDeleteHitCounter resets the server-delete hit counter.
func ResetServerDeleteHitCounter() {
	atomic.StoreInt32(&serverDeleteHits, 0)
}

// ServerDeleteHitCount returns how many times DELETE /servers/{id} was hit.
func ServerDeleteHitCount() int32 {
	return atomic.LoadInt32(&serverDeleteHits)
}

// createdServerImageID records the image ID sent in the most recent POST /servers
// handled by SetupServersEndpointOnMux. Tests use it to assert which image variant
// (e.g. x86 vs. arm) CreateMachine actually selected.
var createdServerImageID int64

// CreatedServerImageID returns the image ID sent in the most recent POST /servers.
func CreatedServerImageID() int64 {
	return atomic.LoadInt64(&createdServerImageID)
}

// TestImageID and TestArmImageID are the IDs of the x86 and arm variants of TestImageName.
const (
	TestImageID    = 42
	TestArmImageID = 43
)

const (
	jsonImageDataTemplate = `
{
	"id": %d,
	"type": "snapshot",
	"status": "available",
	"name": "ubuntu-20.04",
	"description": "Proudly copied from the Hetzner Cloud API documentation",
	"image_size": 2.3,
	"disk_size": 10,
	"created": "2016-01-30T23:50:00+00:00",
	"created_from": {
		"id": 1,
		"name": "Server"
	},
	"os_flavor": "ubuntu",
	"os_version": "20.04",
	"architecture": "%s",
	"rapid_deploy": false,
	"protection": {
		"delete": false
	},
	"deprecated": "2018-02-28T00:00:00+00:00",
	"labels": {}
}
	`
	jsonServerDataTemplate = `
{
	"id": %d,
	"name": "%s",
	"status": "%s",
	"created": "2016-01-30T23:50:00+00:00",
	"public_net": {
		"ipv4": {
			"ip": "1.2.3.4",
			"blocked": false,
			"dns_ptr": "server01.test.invalid"
		},
		"ipv6": {
			"ip": "2001:db8::/64",
			"blocked": false,
			"dns_ptr": [
				{
					"ip": "2001:db8::1",
					"dns_ptr": "server01.test.invalid"
				}
			]
		},
		"floating_ips": [ 42 ]
	},
	"private_net": [
		{
			"network": 1,
			"ip": "10.0.0.2",
			"alias_ips": [],
			"mac_address": "aa:bb:cc:dd:ee:ff"
		}
	],
	"server_type": {
		"id": 1,
		"name": "%s",
		"description": "Test",
		"cores": 1,
		"memory": 1,
		"disk": 25,
		"deprecated": true,
		"prices": [
			{
				"location": "hel1",
				"price_hourly": {
					"net": "1.0000000000",
					"gross": "1.1900000000000000"
				},
				"price_monthly": {
					"net": "1.0000000000",
					"gross": "1.1900000000000000"
				}
			}
		],
		"storage_type": "local",
		"cpu_type": "shared"
	},
	"datacenter": {
		"id": 1,
		"name": "%s",
		"description": "Test",
		"location": {
			"id": 2,
			"name": "hel1",
			"description": "Helsinki DC 2",
			"country": "FI",
			"city": "Helsinki",
			"latitude": 60.1698,
			"longitude": 24.9386,
			"network_zone": "eu-central"
		},
		"server_types": {
			"supported": [ 1, 2, 3 ],
			"available": [ 1, 2, 3 ],
			"available_for_migration": [ 1, 2, 3 ]
		}
	},
	"image": %s,
	"iso": {
		"id": 42,
		"name": "netboot",
		"description": "netboot ISO",
		"type": "public",
		"deprecated": "2018-02-28T00:00:00+00:00"
	},
	"rescue_enabled": false,
	"locked": false,
	"backup_window": "22-02",
	"outgoing_traffic": 123456,
	"ingoing_traffic": 123456,
	"included_traffic": 654321,
	"protection": {
		"delete": false,
		"rebuild": false
	},
	"labels": {},
	"volumes": [],
	"load_balancers": []
}
	`
	TestNamespace            = "test"
	TestServerID             = 42
	TestServerNameTemplate   = "machine-%d"
	testServersLabelSelector = "mcm.gardener.cloud/role=node,topology.kubernetes.io/zone=hel1-dc2"
)

// ManipulateMachine changes given machine data.
//
// PARAMETERS
// machine *v1alpha1.Machine      Machine data
// data    map[string]interface{} Members to change
func ManipulateMachine(machine *v1alpha1.Machine, data map[string]interface{}) *v1alpha1.Machine {
	for key, value := range data {
		if strings.Index(key, "ObjectMeta") == 0 {
			manipulateStruct(&machine.ObjectMeta, key[11:], value)
		} else if strings.Index(key, "Spec") == 0 {
			manipulateStruct(&machine.Spec, key[5:], value)
		} else if strings.Index(key, "Status") == 0 {
			manipulateStruct(&machine.Status, key[7:], value)
		} else if strings.Index(key, "TypeMeta") == 0 {
			manipulateStruct(&machine.TypeMeta, key[9:], value)
		} else {
			manipulateStruct(&machine, key, value)
		}
	}

	return machine
}

// NewMachine generates new v1alpha1 machine data for testing purposes.
//
// PARAMETERS
// serverID int Server ID to use for machine specification
func NewMachine(serverID int) *v1alpha1.Machine {
	index := 0

	if serverID > 0 {
		index = serverID
	}

	machine := &v1alpha1.Machine{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "machine.sapcloud.io",
			Kind:       "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(TestServerNameTemplate, index),
			Namespace: TestNamespace,
		},
	}

	// Don't initialize providerID and node if serverID == -1
	if serverID != -1 {
		machine.Spec = v1alpha1.MachineSpec{
			ProviderID: fmt.Sprintf("hcloud:///%s/%d", TestZone, serverID),
		}
		machine.Status = v1alpha1.MachineStatus{}
	}

	return machine
}

// NewMachineClass generates new v1alpha1 machine class data for testing purposes.
func NewMachineClass() *v1alpha1.MachineClass {
	return NewMachineClassWithProviderSpec([]byte(TestProviderSpec))
}

// NewMachineClassWithProviderSpec generates new v1alpha1 machine class data based on the given provider specification for testing purposes.
//
// PARAMETERS
// providerSpec []byte ProviderSpec to use
func NewMachineClassWithProviderSpec(providerSpec []byte) *v1alpha1.MachineClass {
	return &v1alpha1.MachineClass{
		ProviderSpec: runtime.RawExtension{
			Raw: providerSpec,
		},
	}
}

// newJsonServerData generates a JSON server data object for testing purposes.
//
// PARAMETERS
// serverID    int    Server ID to use
// serverState string Server state to use
func newJsonServerData(serverID int, serverState string) string {
	testServerName := fmt.Sprintf(TestServerNameTemplate, serverID)
	return fmt.Sprintf(jsonServerDataTemplate, serverID, testServerName, serverState, TestServerType, TestZone, newJsonImageData(TestImageID, "x86"))
}

// newJsonImageData generates a JSON image data object for testing purposes.
//
// PARAMETERS
// imageID      int    Image ID to use
// architecture string CPU architecture of the image ("x86" or "arm")
func newJsonImageData(imageID int, architecture string) string {
	return fmt.Sprintf(jsonImageDataTemplate, imageID, architecture)
}

// SetupFloatingIPsEndpointOnMux configures a "/floating_ips" endpoint on the mux given.
//
// PARAMETERS
// mux *http.ServeMux Mux to add handler to
func SetupFloatingIPsEndpointOnMux(mux *http.ServeMux) {
	mux.HandleFunc("/floating_ips", func(res http.ResponseWriter, req *http.Request) {
		res.Header().Add("Content-Type", "application/json; charset=utf-8")

		res.WriteHeader(http.StatusOK)

		if _, err := res.Write([]byte(`
{
	"floating_ips": []
}
		`)); err != nil {
			panic(err)
		}
	})
}

// SetupImagesEndpointOnMux configures a "/images" endpoint on the mux given. Like the
// real API, TestImageName exists once per CPU architecture (x86 first, then arm) and
// the "architecture" query parameter filters the list.
//
// PARAMETERS
// mux *http.ServeMux Mux to add handler to
func SetupImagesEndpointOnMux(mux *http.ServeMux) {
	mux.HandleFunc("/images", func(res http.ResponseWriter, req *http.Request) {
		res.Header().Add("Content-Type", "application/json; charset=utf-8")

		res.WriteHeader(http.StatusOK)

		queryParams := req.URL.Query()
		var images []string

		if queryParams.Get("name") == TestImageName {
			architectures := queryParams["architecture"]

			if len(architectures) == 0 || slices.Contains(architectures, "x86") {
				images = append(images, newJsonImageData(TestImageID, "x86"))
			}
			if len(architectures) == 0 || slices.Contains(architectures, "arm") {
				images = append(images, newJsonImageData(TestArmImageID, "arm"))
			}
		}

		if _, err := fmt.Fprintf(res, "{ \"images\": [ %s ] }", strings.Join(images, ",")); err != nil {
			panic(err)
		}
	})
}

// SetupServerTypesEndpointOnMux configures a "/server_types" endpoint on the mux given
// that knows only TestServerType, with the given CPU architecture.
//
// PARAMETERS
// mux          *http.ServeMux Mux to add handler to
// architecture string         CPU architecture of TestServerType ("x86" or "arm")
func SetupServerTypesEndpointOnMux(mux *http.ServeMux, architecture string) {
	mux.HandleFunc("/server_types", func(res http.ResponseWriter, req *http.Request) {
		res.Header().Add("Content-Type", "application/json; charset=utf-8")

		res.WriteHeader(http.StatusOK)

		serverType := ""

		if req.URL.Query().Get("name") == TestServerType {
			serverType = fmt.Sprintf(`
{
	"id": 1,
	"name": "%s",
	"description": "Test",
	"cores": 1,
	"memory": 1,
	"disk": 25,
	"prices": [],
	"storage_type": "local",
	"cpu_type": "shared",
	"architecture": "%s"
}
			`, TestServerType, architecture)
		}

		if _, err := fmt.Fprintf(res, "{ \"server_types\": [ %s ] }", serverType); err != nil {
			panic(err)
		}
	})
}

// SetupServersEndpointOnMux configures a "/servers" endpoint on the mux given.
//
// PARAMETERS
// mux *http.ServeMux Mux to add handler to
func SetupServersEndpointOnMux(mux *http.ServeMux, emptyOnFirstRequest bool) {
	isEmptyFirstRequest := emptyOnFirstRequest

	mux.HandleFunc("/servers", func(res http.ResponseWriter, req *http.Request) {
		res.Header().Add("Content-Type", "application/json; charset=utf-8")

		if strings.ToLower(req.Method) == "get" {
			res.WriteHeader(http.StatusOK)

			var response bytes.Buffer
			response.Write([]byte(`
{
	"servers": [
			`))

			queryParams := req.URL.Query()

			if queryParams.Get("label_selector") == testServersLabelSelector || queryParams.Get("name") == fmt.Sprintf(TestServerNameTemplate, 0) {
				if emptyOnFirstRequest && isEmptyFirstRequest {
					isEmptyFirstRequest = false
				} else {
					response.Write([]byte(newJsonServerData(TestServerID, "running")))
				}
			}
			response.Write([]byte(`
	]
}
            `))
			if _, err := res.Write(response.Bytes()); err != nil {
				panic(err)
			}
		} else if strings.ToLower(req.Method) == "post" {
			res.WriteHeader(http.StatusCreated)

			jsonData := make([]byte, req.ContentLength)
			req.Body.Read(jsonData)

			var data map[string]interface{}

			jsonErr := json.Unmarshal(jsonData, &data)
			if jsonErr != nil {
				panic(jsonErr)
			}

			if imageID, ok := data["image"].(float64); ok {
				atomic.StoreInt64(&createdServerImageID, int64(imageID))
			}

			jsonServerData := newJsonServerData(TestServerID, "starting")
			if _, err := fmt.Fprintf(res, "{ \"server\": %s, \"root_password\": \"test\" }", jsonServerData); err != nil {
				panic(err)
			}
		} else {
			panic("Unsupported HTTP method call")
		}
	})
}

// SetupSshKeysEndpointOnMux configures a "/ssh_keys" endpoint on the mux given.
//
// PARAMETERS
// mux *http.ServeMux Mux to add handler to
func SetupSshKeysEndpointOnMux(mux *http.ServeMux) {
	mux.HandleFunc("/ssh_keys", func(res http.ResponseWriter, req *http.Request) {
		res.Header().Add("Content-Type", "application/json; charset=utf-8")

		res.WriteHeader(http.StatusOK)

		queryParams := req.URL.Query()
		var response bytes.Buffer
		response.Write([]byte(`
{
	"ssh_keys": [
		`))

		if queryParams.Get("fingerprint") == TestSSHFingerprint {
			response.Write([]byte(`
{
	"id": 42,
	"name": "Simulated ssh key",
	"fingerprint": "00:11:22:33:44:55:66:77:88:99:aa:bb:cc:dd:ee:ff",
	"public_key": "ssh-rsa invalid",
	"labels": {},
	"created": "2016-01-30T23:50:00+00:00"
}
			`))
		}

		response.Write([]byte(`
	]
}
		`))
		if _, err := res.Write(response.Bytes()); err != nil {
			panic(err)
		}
	})
}

// SetupTestPlacementGroupEndpointOnMux configures a "/placement_groups/42" endpoint on the mux given.
//
// PARAMETERS
// mux *http.ServeMux Mux to add handler to
func SetupTestPlacementGroupEndpointOnMux(mux *http.ServeMux) {
	mux.HandleFunc(fmt.Sprintf("/placement_groups/%s", TestPlacementGroupID), func(res http.ResponseWriter, req *http.Request) {
		res.Header().Add("Content-Type", "application/json; charset=utf-8")

		if strings.ToLower(req.Method) != "get" {
			panic("Unsupported HTTP method call")
		}
		atomic.AddInt32(&placementGroupGetByIDHits, 1)
		res.WriteHeader(http.StatusOK)

		res.Write([]byte(`
{
	"placement_group": {
		"created": "2019-01-08T12:10:00+00:00",
		"id": 42,
		"labels": { },
		"name": "Simulated Placement Group",
		"servers": [ ],
		"type": "spread"
	}
}
			`))
	})
}

// SetupTestServerEndpointOnMux configures a "/servers/42" endpoint on the mux given.
//
// PARAMETERS
// mux *http.ServeMux Mux to add handler to
func SetupTestServerEndpointOnMux(mux *http.ServeMux) {
	baseURL := fmt.Sprintf("/servers/%d", TestServerID)

	mux.HandleFunc(baseURL, func(res http.ResponseWriter, req *http.Request) {
		res.Header().Add("Content-Type", "application/json; charset=utf-8")

		if strings.ToLower(req.Method) == "delete" {
			atomic.AddInt32(&serverDeleteHits, 1)
			res.WriteHeader(http.StatusOK)
			res.Write([]byte(fmt.Sprintf(`{ "action": { "id": 13, "command": "delete_server", "status": "running", "progress": 0, "resources": [ { "id": %d, "type": "server" } ] } }`, TestServerID)))
		} else if strings.ToLower(req.Method) == "get" {
			res.WriteHeader(http.StatusOK)
			res.Write([]byte(fmt.Sprintf("{ \"server\": %s }", newJsonServerData(TestServerID, "running"))))
		} else {
			panic("Unsupported HTTP method call")
		}
	})

	mux.HandleFunc(fmt.Sprintf("%s/actions", baseURL), func(res http.ResponseWriter, req *http.Request) {
		res.Header().Add("Content-Type", "application/json; charset=utf-8")

		res.WriteHeader(http.StatusOK)
		res.Write([]byte("{ \"actions\": [] }"))
	})

	mux.HandleFunc(fmt.Sprintf("%s/actions/add_to_placement_group", baseURL), func(res http.ResponseWriter, req *http.Request) {
		res.Header().Add("Content-Type", "application/json; charset=utf-8")

		if strings.ToLower(req.Method) != "post" {
			panic("Unsupported HTTP method call")
		}
		res.WriteHeader(http.StatusCreated)

		jsonData := make([]byte, req.ContentLength)
		req.Body.Read(jsonData)

		var data map[string]interface{}

		jsonErr := json.Unmarshal(jsonData, &data)
		if jsonErr != nil {
			panic(jsonErr)
		}

		if placementGroupID, ok := data["placement_group"]; !ok || testPlacementGroupJsonValue != placementGroupID {
			panic("Invalid HTTP method data")
		}

		atomic.AddInt32(&addToPlacementGroupHits, 1)
		res.Write([]byte("{ \"action\": {} }"))

	})
}
