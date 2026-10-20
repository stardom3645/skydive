package lldp

import (
	"encoding/binary"
	"net"
	"testing"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

func managementAddressTLV(family layers.IANAAddressFamily, address []byte) layers.LinkLayerDiscoveryValue {
	value := append([]byte{byte(len(address) + 1), byte(family)}, address...)
	value = append(value, 2, 0, 0, 0, 10, 0) // interface index 10, no OID
	return layers.LinkLayerDiscoveryValue{Type: layers.LLDPTLVMgmtAddress, Length: uint16(len(value)), Value: value}
}

func TestPreferredManagementAddress(t *testing.T) {
	v4 := managementAddressTLV(layers.IANAAddressFamilyIPV4, net.ParseIP("10.10.30.1").To4())
	v6 := managementAddressTLV(layers.IANAAddressFamilyIPV6, net.ParseIP("fe80::3673:5aff:fe08:3690").To16())
	global := managementAddressTLV(layers.IANAAddressFamilyIPV6, net.ParseIP("2001:db8::1").To16())
	shortV4 := managementAddressTLV(layers.IANAAddressFamilyIPV4, []byte{10, 10})
	truncated := managementAddressTLV(layers.IANAAddressFamilyIPV4, net.ParseIP("10.10.30.1").To4())
	truncated.Value = truncated.Value[:len(truncated.Value)-1]
	badOID := managementAddressTLV(layers.IANAAddressFamilyIPV4, net.ParseIP("10.10.30.1").To4())
	badOID.Value[len(badOID.Value)-1] = 10
	tests := []struct {
		name     string
		values   []layers.LinkLayerDiscoveryValue
		fallback string
		want     string
	}{
		{"Dell IPv4 then IPv6", []layers.LinkLayerDiscoveryValue{v4, v6}, "fe80::3673:5aff:fe08:3690", "10.10.30.1"},
		{"reverse TLV order", []layers.LinkLayerDiscoveryValue{v6, v4}, "10.10.30.1", "10.10.30.1"},
		{"IPv4 only", []layers.LinkLayerDiscoveryValue{v4}, "10.10.30.1", "10.10.30.1"},
		{"IPv6 only", []layers.LinkLayerDiscoveryValue{v6}, "fe80::3673:5aff:fe08:3690", "fe80::3673:5aff:fe08:3690"},
		{"routable IPv6 before link-local", []layers.LinkLayerDiscoveryValue{global, v6}, dellLinkLocalAddress, "2001:db8::1"},
		{"routable IPv6 after link-local", []layers.LinkLayerDiscoveryValue{v6, global}, "2001:db8::1", "2001:db8::1"},
		{"IPv4 before routable IPv6", []layers.LinkLayerDiscoveryValue{v4, global}, "2001:db8::1", "10.10.30.1"},
		{"no addresses", nil, "", ""},
		{"legacy non-IP fallback", nil, "switch.example", "switch.example"},
		{"invalid addresses ignored", []layers.LinkLayerDiscoveryValue{shortV4, truncated, badOID, {Type: layers.LLDPTLVMgmtAddress, Value: []byte{255}}}, "fallback", "fallback"},
		{"unknown final address family", []layers.LinkLayerDiscoveryValue{v4, managementAddressTLV(0, []byte{1, 2, 3, 4})}, "", "10.10.30.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := preferredManagementAddress(tt.values, tt.fallback); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

const dellLinkLocalAddress = "fe80::3673:5aff:fe08:3690"

// Reproduce the actual Dell advertisement ordering through the real decoder,
// rather than testing only pre-decoded data. The legacy identity input must
// remain IPv6 while the address published to the existing UI becomes IPv4.
func TestManagementAddressDellPacket(t *testing.T) {
	var payload []byte
	appendTLV := func(kind layers.LLDPTLVType, value []byte) {
		var header [2]byte
		binary.BigEndian.PutUint16(header[:], uint16(kind)<<9|uint16(len(value)))
		payload = append(payload, header[:]...)
		payload = append(payload, value...)
	}
	appendTLV(layers.LLDPTLVChassisID, []byte{4, 0x34, 0x73, 0x5a, 0x08, 0x36, 0x90})
	appendTLV(layers.LLDPTLVPortID, append([]byte{5}, []byte("ethernet1/1/5")...))
	appendTLV(layers.LLDPTLVTTL, []byte{0, 120})
	for _, tlv := range []layers.LinkLayerDiscoveryValue{
		managementAddressTLV(layers.IANAAddressFamilyIPV4, net.ParseIP("10.10.30.1").To4()),
		managementAddressTLV(layers.IANAAddressFamilyIPV6, net.ParseIP(dellLinkLocalAddress).To16()),
	} {
		appendTLV(tlv.Type, tlv.Value)
	}
	appendTLV(layers.LLDPTLVEnd, nil)
	packet := gopacket.NewPacket(payload, layers.LayerTypeLinkLayerDiscovery, gopacket.Default)
	if err := packet.ErrorLayer(); err != nil {
		t.Fatalf("decode failed: %v", err.Error())
	}
	discovery := packet.Layer(layers.LayerTypeLinkLayerDiscovery).(*layers.LinkLayerDiscovery)
	info := packet.Layer(layers.LayerTypeLinkLayerDiscoveryInfo).(*layers.LinkLayerDiscoveryInfo)
	legacy := net.IP(info.MgmtAddress.Address).String()
	if legacy != dellLinkLocalAddress {
		t.Fatalf("unexpected legacy identity address %q", legacy)
	}
	if got := preferredManagementAddress(discovery.Values, legacy); got != "10.10.30.1" {
		t.Fatalf("published management address: got %q", got)
	}
	if got := net.IP(info.MgmtAddress.Address).String(); got != legacy {
		t.Fatalf("selection mutated legacy identity address: %q", got)
	}
}
