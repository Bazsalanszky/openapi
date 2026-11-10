// SPDX-FileCopyrightText: 2026 Balázs Toldi
//
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/omec-project/openapi/v2/models"
)

// NFProfileAddresses is the part of an NF profile that says where the NF's
// services can be reached. *models.NFProfile and *models.NFProfileDiscovery
// both implement it.
type NFProfileAddresses interface {
	GetFqdn() string
	GetIpv4Addresses() []string
	GetIpv6Addresses() []string
	GetNfServices() []models.NFService
	GetNfServiceList() map[string]models.NFService
}

// NFProfileServices returns the profile's NF services, preferring the TS
// 29.510 Rel-16 nfServiceList over the deprecated nfServices array.
// nfServiceList is a map, so its entries are returned sorted by key (the
// service instance ID) to make the choice between matching services
// deterministic.
func NFProfileServices(profile NFProfileAddresses) []models.NFService {
	if profile == nil {
		return nil
	}
	serviceList := profile.GetNfServiceList()
	if len(serviceList) == 0 {
		return profile.GetNfServices()
	}
	ids := make([]string, 0, len(serviceList))
	for id := range serviceList {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	services := make([]models.NFService, 0, len(ids))
	for _, id := range ids {
		services = append(services, serviceList[id])
	}
	return services
}

// SearchNFServiceURI returns the base URI of the first service of the
// profile with the given name and status, as NFServiceURI builds it, or ""
// when no matching service yields one.
func SearchNFServiceURI(profile NFProfileAddresses, serviceName models.ServiceName,
	status models.NFServiceStatus, preferIPv6 bool,
) string {
	for _, service := range NFProfileServices(profile) {
		if service.GetServiceName() != serviceName || service.GetNfServiceStatus() != status {
			continue
		}
		if uri := NFServiceURI(profile, &service, preferIPv6); uri != "" {
			return uri
		}
	}
	return ""
}

// NFServiceURI returns the base URI (apiRoot) of one service of a discovered
// NF. It uses, in order:
//  1. the profile fqdn,
//  2. the service fqdn,
//  3. the service apiPrefix, returned as it is,
//  4. the address of an ipEndPoint, preferring the family chosen by
//     preferIPv6,
//  5. the first profile address of the preferred family, else of the other
//     one, when the service has ipEndPoints but none carries an address.
//
// Apart from the apiPrefix case the result is always scheme://host:port,
// with an IPv6 host in brackets. The port comes from the matching ipEndPoint
// (cases 1, 2 and 5: the first one); when it is 0 or absent the default port
// of the scheme is used (443 for https, otherwise 80). An empty scheme is
// taken as http. It returns "" when none of the above is present.
func NFServiceURI(profile NFProfileAddresses, service *models.NFService, preferIPv6 bool) string {
	if profile == nil || service == nil {
		return ""
	}
	scheme := string(service.GetScheme())
	endPoints := service.GetIpEndPoints()
	var port int32
	if len(endPoints) > 0 {
		port = endPoints[0].GetPort()
	}
	if fqdn := profile.GetFqdn(); fqdn != "" {
		return sbiURI(scheme, fqdn, port)
	}
	if fqdn := service.GetFqdn(); fqdn != "" {
		return sbiURI(scheme, fqdn, port)
	}
	if apiPrefix := service.GetApiPrefix(); apiPrefix != "" {
		return apiPrefix
	}
	if host, endPointPort, ok := endPointAddress(endPoints, preferIPv6); ok {
		return sbiURI(scheme, host, endPointPort)
	}
	if len(endPoints) == 0 {
		return ""
	}
	if host := preferredAddress(profile.GetIpv4Addresses(), profile.GetIpv6Addresses(), preferIPv6); host != "" {
		return sbiURI(scheme, host, port)
	}
	return ""
}

// endPointAddress returns the first ipEndPoint address of the preferred
// family, else the first of the other family, with that endpoint's port.
func endPointAddress(endPoints []models.IpEndPoint, preferIPv6 bool) (string, int32, bool) {
	families := []bool{preferIPv6, !preferIPv6}
	for _, ipv6 := range families {
		for i := range endPoints {
			address := endPoints[i].GetIpv4Address()
			if ipv6 {
				address = endPoints[i].GetIpv6Address()
			}
			if address != "" {
				return address, endPoints[i].GetPort(), true
			}
		}
	}
	return "", 0, false
}

func preferredAddress(ipv4, ipv6 []string, preferIPv6 bool) string {
	if preferIPv6 {
		ipv4, ipv6 = ipv6, ipv4
	}
	for _, list := range [][]string{ipv4, ipv6} {
		for _, address := range list {
			if address != "" {
				return address
			}
		}
	}
	return ""
}

// sbiURI builds scheme://host:port with an IPv6 host in brackets (and its
// zone, if any, percent-encoded as RFC 6874 requires). A host that is
// already bracketed is accepted. Host names and IPv4 addresses are used as
// they are.
func sbiURI(scheme, host string, port int32) string {
	if scheme == "" {
		scheme = string(models.URISCHEME_HTTP)
	}
	if port == 0 {
		port = 80
		if scheme == string(models.URISCHEME_HTTPS) {
			port = 443
		}
	}
	if len(host) >= 2 && host[0] == '[' && host[len(host)-1] == ']' {
		host = host[1 : len(host)-1]
	}
	if strings.Contains(host, ":") && !strings.Contains(host, "%25") {
		host = strings.Replace(host, "%", "%25", 1)
	}
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(int(port)))
}

// NewIpEndPoints returns the TCP ipEndPoints of an NF service listening on
// port. TS 29.510 allows at most one of ipv4Address and ipv6Address in an
// IpEndPoint, so a dual-stack NF gets two endpoints, IPv4 first (consumers
// that only look at the first endpoint keep using IPv4). With neither address
// (an NF registered by FQDN) it returns one endpoint carrying only the port.
func NewIpEndPoints(ipv4, ipv6 string, port int32) []models.IpEndPoint {
	newEndPoint := func() models.IpEndPoint {
		endPoint := models.NewIpEndPoint()
		endPoint.SetTransport(models.TRANSPORTPROTOCOL_TCP)
		endPoint.SetPort(port)
		return *endPoint
	}
	var endPoints []models.IpEndPoint
	if ipv4 != "" {
		endPoint := newEndPoint()
		endPoint.SetIpv4Address(ipv4)
		endPoints = append(endPoints, endPoint)
	}
	if ipv6 != "" {
		endPoint := newEndPoint()
		endPoint.SetIpv6Address(ipv6)
		endPoints = append(endPoints, endPoint)
	}
	if len(endPoints) == 0 {
		endPoints = append(endPoints, newEndPoint())
	}
	return endPoints
}

// SetNFProfileAddresses sets the fqdn, ipv4Addresses and ipv6Addresses of an
// NF profile to the given values. An empty value leaves its field as it is,
// so an NF that only has an IPv4 address produces the same profile as
// before.
func SetNFProfileAddresses(profile *models.NFProfile, fqdn, ipv4, ipv6 string) {
	if profile == nil {
		return
	}
	if fqdn != "" {
		profile.SetFqdn(fqdn)
	}
	if ipv4 != "" {
		profile.SetIpv4Addresses([]string{ipv4})
	}
	if ipv6 != "" {
		profile.SetIpv6Addresses([]string{ipv6})
	}
}
