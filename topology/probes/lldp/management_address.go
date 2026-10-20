package lldp

import (
	"net"

	"github.com/google/gopacket/layers"
)

// preferredManagementAddress reads every management-address TLV: gopacket's
// LinkLayerDiscoveryInfo.MgmtAddress retains only the last one. Keep the existing
// single-address metadata field, preferring IPv4, then non-link-local IPv6, then
// link-local IPv6. The legacy decoded address remains the fallback and identity
// discriminator; changing this preference must not change graph node IDs.
func preferredManagementAddress(values []layers.LinkLayerDiscoveryValue, fallback string) string {
	preferred, bestRank := fallback, 0
	for _, tlv := range values {
		if tlv.Type != layers.LLDPTLVMgmtAddress || len(tlv.Value) < 9 {
			continue
		}
		value := tlv.Value
		// Address length includes the address-family byte. The remainder is
		// interface subtype (1), interface number (4), OID length (1), and OID.
		length := int(value[0])
		if length < 2 || len(value) < length+7 || len(value) < length+7+int(value[length+6]) {
			continue
		}
		address := value[2 : length+1]
		var rank int
		switch layers.IANAAddressFamily(value[1]) {
		case layers.IANAAddressFamilyIPV4:
			if len(address) != net.IPv4len {
				continue
			}
			rank = 3
		case layers.IANAAddressFamilyIPV6:
			if len(address) != net.IPv6len {
				continue
			}
			rank = 2
			if net.IP(address).IsLinkLocalUnicast() {
				rank = 1
			}
		default:
			continue
		}
		ip := net.IP(address)
		if ip.IsUnspecified() || ip.IsMulticast() {
			continue
		}
		if rank > bestRank {
			preferred, bestRank = ip.String(), rank
		}
	}
	return preferred
}
