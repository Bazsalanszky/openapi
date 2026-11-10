// SPDX-FileCopyrightText: 2026 Balázs Toldi
//
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/omec-project/openapi/v2/models"
)

const (
	testV4      = "192.0.2.10"
	testV4b     = "192.0.2.20"
	testV6      = "2001:db8::10"
	testV6b     = "2001:db8::20"
	testHost    = "udm"
	testFqdn    = "udm.omec.svc.cluster.local"
	testPort    = int32(29503)
	testService = models.SERVICENAME_NUDM_SDM
	testUdmURI  = "http://udm:29503"
	tcp         = string(models.TRANSPORTPROTOCOL_TCP)
)

type endPointSpec struct {
	ipv4 string
	ipv6 string
	port int32
}

type serviceSpec struct {
	scheme    models.UriScheme
	fqdn      string
	apiPrefix string
	endPoints []endPointSpec
}

func newService(spec serviceSpec) models.NFService {
	service := models.NFService{
		ServiceInstanceId: "0",
		ServiceName:       testService,
		Scheme:            spec.scheme,
		NfServiceStatus:   models.NFSERVICESTATUS_REGISTERED,
	}
	if spec.fqdn != "" {
		service.SetFqdn(spec.fqdn)
	}
	if spec.apiPrefix != "" {
		service.SetApiPrefix(spec.apiPrefix)
	}
	for _, e := range spec.endPoints {
		endPoint := models.NewIpEndPoint()
		if e.ipv4 != "" {
			endPoint.SetIpv4Address(e.ipv4)
		}
		if e.ipv6 != "" {
			endPoint.SetIpv6Address(e.ipv6)
		}
		if e.port != 0 {
			endPoint.SetPort(e.port)
		}
		service.IpEndPoints = append(service.IpEndPoints, *endPoint)
	}
	return service
}

func newProfile(fqdn string, ipv4, ipv6 []string, services ...models.NFService) *models.NFProfileDiscovery {
	profile := models.NewNFProfileDiscoveryWithDefaults()
	if fqdn != "" {
		profile.SetFqdn(fqdn)
	}
	profile.Ipv4Addresses = ipv4
	profile.Ipv6Addresses = ipv6
	profile.NfServices = services
	return profile
}

func TestNFServiceURI(t *testing.T) {
	http, https := models.URISCHEME_HTTP, models.URISCHEME_HTTPS
	v4EndPoint := []endPointSpec{{ipv4: testV4, port: testPort}}
	portOnly := []endPointSpec{{port: testPort}}

	tests := []struct {
		name       string
		profFqdn   string
		want       string
		profV4     []string
		profV6     []string
		service    serviceSpec
		preferIPv6 bool
	}{
		{name: "nothing", service: serviceSpec{scheme: http}},

		// 1. profile fqdn
		{
			name: "profile fqdn with endpoint port", profFqdn: testHost,
			service: serviceSpec{scheme: http, endPoints: v4EndPoint},
			want:    testUdmURI,
		},
		{
			name: "profile fqdn wins over everything", profFqdn: testFqdn, profV4: []string{testV4b},
			service: serviceSpec{scheme: http, fqdn: "other", apiPrefix: "http://prefix:1", endPoints: v4EndPoint},
			want:    "http://" + testFqdn + ":29503",
		},
		{name: "profile fqdn, http default port", profFqdn: testHost, service: serviceSpec{scheme: http}, want: "http://udm:80"},
		{name: "profile fqdn, https default port", profFqdn: testHost, service: serviceSpec{scheme: https}, want: "https://udm:443"},
		{name: "empty scheme is http", profFqdn: testHost, service: serviceSpec{endPoints: portOnly}, want: testUdmURI},

		// 2. service fqdn
		{
			name:    "service fqdn",
			service: serviceSpec{scheme: https, fqdn: testFqdn, apiPrefix: "http://prefix:1", endPoints: v4EndPoint},
			want:    "https://" + testFqdn + ":29503",
		},

		// 3. apiPrefix
		{
			name:    "apiPrefix is used as it is",
			service: serviceSpec{scheme: http, apiPrefix: testUdmURI, endPoints: v4EndPoint},
			want:    testUdmURI,
		},
		{
			name:    "IPv6 apiPrefix is used as it is",
			service: serviceSpec{scheme: http, apiPrefix: "http://[" + testV6 + "]:29503"},
			want:    "http://[" + testV6 + "]:29503",
		},

		// 4. ipEndPoints
		{name: "IPv4 endpoint", service: serviceSpec{scheme: http, endPoints: v4EndPoint}, want: "http://" + testV4 + ":29503"},
		{
			name:    "IPv6 endpoint is bracketed",
			service: serviceSpec{scheme: http, endPoints: []endPointSpec{{ipv6: testV6, port: testPort}}},
			want:    "http://[" + testV6 + "]:29503",
		},
		{
			name:    "IPv6 endpoint with IPv4 preference",
			service: serviceSpec{scheme: http, endPoints: []endPointSpec{{ipv6: testV6, port: testPort}}},
			want:    "http://[" + testV6 + "]:29503",
		},
		{
			name:    "bracketed IPv6 endpoint",
			service: serviceSpec{scheme: http, endPoints: []endPointSpec{{ipv6: "[" + testV6 + "]", port: testPort}}},
			want:    "http://[" + testV6 + "]:29503",
		},
		{
			name:    "zoned IPv6 endpoint",
			service: serviceSpec{scheme: http, endPoints: []endPointSpec{{ipv6: "fe80::1%eth0", port: testPort}}},
			want:    "http://[fe80::1%25eth0]:29503",
		},
		{
			name:    "dual endpoints, IPv4 preferred",
			service: serviceSpec{scheme: http, endPoints: []endPointSpec{{ipv4: testV4, port: 1}, {ipv6: testV6, port: 2}}},
			want:    "http://" + testV4 + ":1",
		},
		{
			name:    "dual endpoints, IPv6 preferred",
			service: serviceSpec{scheme: http, endPoints: []endPointSpec{{ipv4: testV4, port: 1}, {ipv6: testV6, port: 2}}},
			want:    "http://[" + testV6 + "]:2", preferIPv6: true,
		},
		{
			name:    "family wins over endpoint order",
			service: serviceSpec{scheme: http, endPoints: []endPointSpec{{ipv6: testV6, port: 2}, {ipv4: testV4, port: 1}}},
			want:    "http://" + testV4 + ":1",
		},
		{
			name:    "IPv4 endpoint with IPv6 preference",
			service: serviceSpec{scheme: http, endPoints: v4EndPoint},
			want:    "http://" + testV4 + ":29503", preferIPv6: true,
		},
		{
			name:    "later endpoint with an address",
			service: serviceSpec{scheme: http, endPoints: []endPointSpec{{port: 1}, {ipv4: testV4, port: 2}}},
			want:    "http://" + testV4 + ":2",
		},
		{
			name:    "host name in ipv4Address (what several NFs register today)",
			service: serviceSpec{scheme: http, endPoints: []endPointSpec{{ipv4: testHost, port: testPort}}},
			want:    testUdmURI,
		},
		{
			name:    "IPv6 in ipv4Address is still bracketed",
			service: serviceSpec{scheme: http, endPoints: []endPointSpec{{ipv4: testV6, port: testPort}}},
			want:    "http://[" + testV6 + "]:29503",
		},
		{
			name:    "endpoint without port, https",
			service: serviceSpec{scheme: https, endPoints: []endPointSpec{{ipv6: testV6}}},
			want:    "https://[" + testV6 + "]:443",
		},
		{
			name:    "endpoint without port, http",
			service: serviceSpec{scheme: http, endPoints: []endPointSpec{{ipv4: testV4}}},
			want:    "http://" + testV4 + ":80",
		},

		// 5. profile addresses
		{
			name: "profile IPv4", profV4: []string{testV4b},
			service: serviceSpec{scheme: http, endPoints: portOnly},
			want:    "http://" + testV4b + ":29503",
		},
		{
			name: "profile IPv6 only", profV6: []string{testV6b},
			service: serviceSpec{scheme: http, endPoints: portOnly},
			want:    "http://[" + testV6b + "]:29503",
		},
		{
			name: "profile dual, IPv4 preferred", profV4: []string{testV4b}, profV6: []string{testV6b},
			service: serviceSpec{scheme: http, endPoints: portOnly},
			want:    "http://" + testV4b + ":29503",
		},
		{
			name: "profile dual, IPv6 preferred", profV4: []string{testV4b}, profV6: []string{testV6b},
			service: serviceSpec{scheme: http, endPoints: portOnly},
			want:    "http://[" + testV6b + "]:29503", preferIPv6: true,
		},
		{
			name: "empty profile entries are skipped", profV4: []string{"", testV4b},
			service: serviceSpec{scheme: http, endPoints: portOnly},
			want:    "http://" + testV4b + ":29503",
		},
		{
			name: "endpoint address wins over profile address", profV4: []string{testV4b},
			service: serviceSpec{scheme: http, endPoints: []endPointSpec{{ipv6: testV6, port: testPort}}},
			want:    "http://[" + testV6 + "]:29503",
		},
		{
			name: "profile address needs an endpoint, as before", profV4: []string{testV4b},
			service: serviceSpec{scheme: http},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service := newService(tc.service)
			profile := newProfile(tc.profFqdn, tc.profV4, tc.profV6)
			got := NFServiceURI(profile, &service, tc.preferIPv6)
			if got != tc.want {
				t.Fatalf("NFServiceURI() = %q, want %q", got, tc.want)
			}
			if got == "" {
				return
			}
			if _, err := url.Parse(got); err != nil {
				t.Fatalf("result %q does not parse: %v", got, err)
			}
		})
	}
}

func TestNFServiceURINil(t *testing.T) {
	service := newService(serviceSpec{scheme: models.URISCHEME_HTTP, endPoints: []endPointSpec{{ipv4: testV4}}})
	if got := NFServiceURI(nil, &service, false); got != "" {
		t.Errorf("nil profile: %q", got)
	}
	if got := NFServiceURI(newProfile("", nil, nil), nil, false); got != "" {
		t.Errorf("nil service: %q", got)
	}
	var nilProfile *models.NFProfile
	if got := NFServiceURI(nilProfile, &service, false); got != "http://"+testV4+":80" {
		t.Errorf("typed nil profile: %q", got)
	}
}

func TestSearchNFServiceURI(t *testing.T) {
	http := models.URISCHEME_HTTP
	named := func(id string, name models.ServiceName, status models.NFServiceStatus, spec serviceSpec) models.NFService {
		service := newService(spec)
		service.ServiceInstanceId = id
		service.ServiceName = name
		service.NfServiceStatus = status
		return service
	}
	v4 := serviceSpec{scheme: http, endPoints: []endPointSpec{{ipv4: testV4, port: 1}}}
	v6 := serviceSpec{scheme: http, endPoints: []endPointSpec{{ipv6: testV6, port: 2}}}
	none := serviceSpec{scheme: http}
	registered, suspended := models.NFSERVICESTATUS_REGISTERED, models.NFSERVICESTATUS_SUSPENDED

	t.Run("name and status must match", func(t *testing.T) {
		profile := newProfile("", nil, nil,
			named("a", models.SERVICENAME_NUDM_UECM, registered, v4),
			named("b", testService, suspended, v4),
			named("c", testService, registered, v6),
		)
		if got, want := SearchNFServiceURI(profile, testService, registered, false), "http://["+testV6+"]:2"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
	t.Run("a match without an address is skipped", func(t *testing.T) {
		profile := newProfile("", nil, nil, named("a", testService, registered, none), named("b", testService, registered, v4))
		if got, want := SearchNFServiceURI(profile, testService, registered, false), "http://"+testV4+":1"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
	t.Run("no match", func(t *testing.T) {
		profile := newProfile("", nil, nil, named("a", testService, suspended, v4))
		if got := SearchNFServiceURI(profile, testService, registered, false); got != "" {
			t.Fatalf("got %q", got)
		}
		if got := SearchNFServiceURI(nil, testService, registered, false); got != "" {
			t.Fatalf("nil profile: got %q", got)
		}
	})
	t.Run("nfServiceList wins and is ordered by key", func(t *testing.T) {
		profile := newProfile("", nil, nil, named("z", testService, registered, v4))
		profile.SetNfServiceList(map[string]models.NFService{
			"2": named("2", testService, registered, v4),
			"1": named("1", testService, registered, v6),
		})
		for range 20 {
			if got, want := SearchNFServiceURI(profile, testService, registered, false), "http://["+testV6+"]:2"; got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		}
	})
	t.Run("works on NFProfile too", func(t *testing.T) {
		profile := models.NewNFProfileWithDefaults()
		profile.SetFqdn(testHost)
		profile.NfServices = []models.NFService{named("a", testService, registered, v4)}
		if got, want := SearchNFServiceURI(profile, testService, registered, true), "http://udm:1"; got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})
}

// legacySearchNFServiceUri is amf util/search_nf_service.go at upstream
// 1a9146c, the most complete of the copies in the NFs.
func legacySearchNFServiceUri(nfProfile models.NFProfileDiscovery, serviceName models.ServiceName,
	nfServiceStatus models.NFServiceStatus,
) (nfUri string) {
	legacyURI := func(scheme models.UriScheme, ipv4Address string, port int32) string {
		if port != 0 {
			return fmt.Sprintf("%s://%s:%d", scheme, ipv4Address, port)
		}
		if scheme == models.URISCHEME_HTTPS {
			return fmt.Sprintf("%s://%s:443", scheme, ipv4Address)
		}
		return fmt.Sprintf("%s://%s:80", models.URISCHEME_HTTP, ipv4Address)
	}
	services := NFProfileServices(&nfProfile)
	for _, service := range services {
		if service.GetServiceName() == serviceName && service.GetNfServiceStatus() == nfServiceStatus {
			port := int32(0)
			if len(service.GetIpEndPoints()) > 0 {
				port = service.GetIpEndPoints()[0].GetPort()
			}
			if nfProfile.GetFqdn() != "" {
				nfUri = legacyURI(service.GetScheme(), nfProfile.GetFqdn(), port)
			} else if service.GetFqdn() != "" {
				nfUri = legacyURI(service.GetScheme(), service.GetFqdn(), port)
			} else if service.GetApiPrefix() != "" {
				nfUri = service.GetApiPrefix()
			} else if len(service.GetIpEndPoints()) > 0 {
				point := service.GetIpEndPoints()[0]
				if point.GetIpv4Address() != "" {
					nfUri = legacyURI(service.GetScheme(), point.GetIpv4Address(), point.GetPort())
				} else if len(nfProfile.GetIpv4Addresses()) != 0 {
					nfUri = legacyURI(service.GetScheme(), nfProfile.GetIpv4Addresses()[0], point.GetPort())
				}
			}
		}
		if nfUri != "" {
			break
		}
	}
	return nfUri
}

// TestSearchNFServiceURIKeepsLegacyResults checks that for IPv4-only and
// FQDN profiles with a scheme set, the shared resolver returns what the AMF
// copy returns today, whatever the preference.
func TestSearchNFServiceURIKeepsLegacyResults(t *testing.T) {
	var profiles []*models.NFProfileDiscovery
	for _, scheme := range []models.UriScheme{models.URISCHEME_HTTP, models.URISCHEME_HTTPS} {
		for _, profFqdn := range []string{"", testHost} {
			for _, profV4 := range [][]string{nil, {testV4b}} {
				for _, svcFqdn := range []string{"", testFqdn} {
					for _, apiPrefix := range []string{"", testUdmURI} {
						for _, endPoints := range [][]endPointSpec{
							nil,
							{{port: testPort}},
							{{port: 0}},
							{{ipv4: testV4, port: testPort}},
							{{ipv4: testV4}},
							{{ipv4: testHost, port: testPort}},
						} {
							profiles = append(profiles, newProfile(profFqdn, profV4, nil, newService(serviceSpec{
								scheme: scheme, fqdn: svcFqdn, apiPrefix: apiPrefix, endPoints: endPoints,
							})))
						}
					}
				}
			}
		}
	}
	for i, profile := range profiles {
		want := legacySearchNFServiceUri(*profile, testService, models.NFSERVICESTATUS_REGISTERED)
		for _, preferIPv6 := range []bool{false, true} {
			if got := SearchNFServiceURI(profile, testService, models.NFSERVICESTATUS_REGISTERED, preferIPv6); got != want {
				t.Errorf("profile %d %+v (preferIPv6=%v): got %q, legacy %q", i, *profile, preferIPv6, got, want)
			}
		}
	}
}

func TestNewIpEndPoints(t *testing.T) {
	// endPoint is the JSON form of a TCP endpoint on testPort, plus fields.
	endPoint := func(fields ...string) map[string]any {
		m := map[string]any{"port": float64(testPort), "transport": tcp}
		for k := 0; k+1 < len(fields); k += 2 {
			m[fields[k]] = fields[k+1]
		}
		return m
	}
	tests := []struct {
		name string
		ipv4 string
		ipv6 string
		want []map[string]any
	}{
		{name: "IPv4", ipv4: testV4, want: []map[string]any{endPoint("ipv4Address", testV4)}},
		{name: "IPv6", ipv6: testV6, want: []map[string]any{endPoint("ipv6Address", testV6)}},
		{
			name: "dual stack, IPv4 first", ipv4: testV4, ipv6: testV6,
			want: []map[string]any{endPoint("ipv4Address", testV4), endPoint("ipv6Address", testV6)},
		},
		{name: "FQDN only: port only", want: []map[string]any{endPoint()}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b, err := json.Marshal(NewIpEndPoints(tc.ipv4, tc.ipv6, testPort))
			if err != nil {
				t.Fatal(err)
			}
			var got []map[string]any
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %s, want %v", b, tc.want)
			}
			if strings.Contains(string(b), `"ipv4Address":"`+testV4+`","ipv6Address"`) {
				t.Fatalf("an endpoint carries both families: %s", b)
			}
		})
	}
}

func TestSetNFProfileAddresses(t *testing.T) {
	tests := []struct {
		name     string
		fqdn     string
		ipv4     string
		ipv6     string
		wantFqdn string
		wantV4   []string
		wantV6   []string
	}{
		{name: "nothing"},
		{name: "IPv4", ipv4: testV4, wantV4: []string{testV4}},
		{name: "IPv6", ipv6: testV6, wantV6: []string{testV6}},
		{name: "dual", ipv4: testV4, ipv6: testV6, wantV4: []string{testV4}, wantV6: []string{testV6}},
		{name: "FQDN and IPv6", fqdn: testHost, ipv6: testV6, wantFqdn: testHost, wantV6: []string{testV6}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			profile := models.NewNFProfileWithDefaults()
			SetNFProfileAddresses(profile, tc.fqdn, tc.ipv4, tc.ipv6)
			if profile.GetFqdn() != tc.wantFqdn || !reflect.DeepEqual(profile.Ipv4Addresses, tc.wantV4) ||
				!reflect.DeepEqual(profile.Ipv6Addresses, tc.wantV6) {
				t.Fatalf("got fqdn %q, ipv4 %v, ipv6 %v", profile.GetFqdn(), profile.Ipv4Addresses, profile.Ipv6Addresses)
			}
		})
	}
	SetNFProfileAddresses(nil, testHost, testV4, testV6) // must not panic
}
