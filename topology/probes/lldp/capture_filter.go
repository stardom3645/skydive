package lldp

// Capture 8192 bytes so that we have the full Ethernet frame.
const lldpSnapLen = 8192

// Accept LLDP with zero, one or two 802.1Q/802.1ad VLAN headers. Use explicit
// Ethernet offsets because libpcap's "vlan" keyword shifts offsets for the rest
// of an expression, which can affect shared source/destination MAC checks.
// Hardware-stripped VLAN frames still match the untagged/single-tag cases.
const lldpBPFFilter = `ether[0] & 1 = 1 and
  !(ether src %s) and
  (ether dst 01:80:c2:00:00:0e or
   ether dst 01:80:c2:00:00:03 or
   ether dst 01:80:c2:00:00:00) and
  (ether[12:2] = 0x88cc or
   ((ether[12:2] = 0x8100 or ether[12:2] = 0x88a8) and
    ether[16:2] = 0x88cc) or
   ((ether[12:2] = 0x8100 or ether[12:2] = 0x88a8) and
    (ether[16:2] = 0x8100 or ether[16:2] = 0x88a8) and
    ether[20:2] = 0x88cc))`
