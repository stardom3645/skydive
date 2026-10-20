//go:build cgo && (linux || darwin)
// +build cgo
// +build linux darwin

package lldp

import (
	"encoding/binary"
	"fmt"
	"net"
	"testing"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
)

func lldpFilterFrame(source, destination string, tags []uint16, etherType uint16) []byte {
	dst, _ := net.ParseMAC(destination)
	src, _ := net.ParseMAC(source)
	frame := append(dst, src...)
	for index, tag := range tags {
		frame = binary.BigEndian.AppendUint16(frame, tag)
		frame = binary.BigEndian.AppendUint16(frame, uint16(101+index))
	}
	frame = binary.BigEndian.AppendUint16(frame, etherType)
	add := func(kind layers.LLDPTLVType, value []byte) {
		frame = binary.BigEndian.AppendUint16(frame, uint16(kind)<<9|uint16(len(value)))
		frame = append(frame, value...)
	}
	add(layers.LLDPTLVChassisID, []byte{4, 0x34, 0x73, 0x5a, 0x08, 0x36, 0x90})
	add(layers.LLDPTLVPortID, append([]byte{5}, []byte("ethernet1/1/2")...))
	add(layers.LLDPTLVTTL, []byte{0, 120})
	add(layers.LLDPTLVPortDescription, []byte("ethernet1/1/2"))
	add(layers.LLDPTLVSysName, []byte("OS10"))
	for _, address := range []layers.LinkLayerDiscoveryValue{
		managementAddressTLV(layers.IANAAddressFamilyIPV4, net.ParseIP("10.10.30.1").To4()),
		managementAddressTLV(layers.IANAAddressFamilyIPV6, net.ParseIP(dellLinkLocalAddress).To16()),
	} {
		add(address.Type, address.Value)
	}
	add(layers.LLDPTLVEnd, nil)
	return frame
}

func TestLLDPCaptureFilter(t *testing.T) {
	const localMAC = "20:3a:43:01:a3:c0"
	const switchMAC = "34:73:5a:08:36:92"
	filter, err := pcap.NewBPF(layers.LinkTypeEthernet, lldpSnapLen, fmt.Sprintf(lldpBPFFilter, localMAC))
	if err != nil {
		t.Fatal(err)
	}
	matches := func(frame []byte) bool {
		return filter.Matches(gopacket.CaptureInfo{CaptureLength: len(frame), Length: len(frame)}, frame)
	}
	tagCases := []struct {
		name string
		tags []uint16
	}{
		{"untagged", nil},
		{"802.1Q", []uint16{0x8100}},
		{"802.1ad", []uint16{0x88a8}},
		{"Q-in-Q", []uint16{0x8100, 0x8100}},
		{"802.1ad plus 802.1Q", []uint16{0x88a8, 0x8100}},
		{"802.1Q plus 802.1ad", []uint16{0x8100, 0x88a8}},
	}
	for _, tc := range tagCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, destination := range []string{"01:80:c2:00:00:0e", "01:80:c2:00:00:03", "01:80:c2:00:00:00"} {
				frame := lldpFilterFrame(switchMAC, destination, tc.tags, 0x88cc)
				if !matches(frame) {
					t.Fatalf("switch LLDP rejected for %s", destination)
				}
				packet := gopacket.NewPacket(frame, layers.LayerTypeEthernet, gopacket.Default)
				if packet.ErrorLayer() != nil || packet.Layer(layers.LayerTypeLinkLayerDiscovery) == nil {
					t.Fatalf("accepted LLDP cannot be decoded: %v", packet.ErrorLayer())
				}
				discovery := packet.Layer(layers.LayerTypeLinkLayerDiscovery).(*layers.LinkLayerDiscovery)
				if string(discovery.PortID.ID) != "ethernet1/1/2" || discovery.TTL != 120 {
					t.Fatal("VLAN headers changed decoded port identity or TTL")
				}
				info := packet.Layer(layers.LayerTypeLinkLayerDiscoveryInfo).(*layers.LinkLayerDiscoveryInfo)
				if address := preferredManagementAddress(discovery.Values, net.IP(info.MgmtAddress.Address).String()); address != "10.10.30.1" {
					t.Fatalf("decoded switch management address: %s", address)
				}
				if matches(lldpFilterFrame(localMAC, destination, tc.tags, 0x88cc)) {
					t.Fatal("local advertisement accepted as remote neighbor")
				}
				for _, protocol := range []uint16{0x0800, 0x86dd, 0x0806} {
					if matches(lldpFilterFrame(switchMAC, destination, tc.tags, protocol)) {
						t.Fatalf("non-LLDP EtherType %#x accepted", protocol)
					}
				}
			}
			for _, destination := range []string{localMAC, "ff:ff:ff:ff:ff:ff", "01:80:c2:00:00:0f"} {
				if matches(lldpFilterFrame(switchMAC, destination, tc.tags, 0x88cc)) {
					t.Fatalf("unsupported destination accepted: %s", destination)
				}
			}
			frame := lldpFilterFrame(switchMAC, "01:80:c2:00:00:0e", tc.tags, 0x88cc)
			// libpcap's Go wrapper requires a non-empty packet buffer.
			for length := 1; length < 14+4*len(tc.tags); length++ {
				if matches(frame[:length]) {
					t.Fatalf("truncated Ethernet/VLAN header accepted at length %d", length)
				}
			}
		})
	}
}
