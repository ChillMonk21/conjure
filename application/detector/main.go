package main

import (
	"bytes"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
)

// Detector implements a single thread packet capture process forming a critical
// piece of a refraction networking station. This simple detector is independent
// of the Tapdance style registration components of the more heavyweight Rust
// detector implementation and is (at present) meant purely for testing and use
// with the API based registrars.
type Detector struct {
	// interface to listen on
	Iface string

	// List of addresses to filter packets from (i.e. liveness testing)
	FilterList []string

	// Check if a packet is registered based on the destination address
	IsRegistered func(src, dst string, dstPort uint16) bool

	// Tags checked for routing investigation purposes.
	Tags []string

	// Logger provided by initializing application.
	Logger *log.Logger

	// bool for independent thread to synchronize exit.
	exit bool

	// How often to log
	StatsFrequency int

	// TODO
	// Stats tracking to mimic rust station
	stats *DetectorStats
}

// Run sets the detector running, capturing traffic and processing checking for
// connections associated with registrations.
func (det *Detector) Run() {

	if !deviceExists(det.Iface) {
		log.Fatal("Unable to open device ", iface)
	}

	// Open packet reader in promiscuous mode.
	handler, err := pcap.OpenLive(det.Iface, buffer, false, pcap.BlockForever)
	if err != nil {
		log.Fatal(err)
	}
	defer handler.Close()

	//Generate and Apply filters
	filter := generateFilters(det.FilterList)
	if err := handler.SetBPFFilter(filter); err != nil {
		log.Fatal(err)
	}

	go det.spawnStatsThread()

	// Actually process packets
	source := gopacket.NewPacketSource(handler, handler.LinkType())
	for packet := range source.Packets() {
		det.handlePacket(packet)
	}

	det.exit = true
	det.Logger.Printf("Detector Shutting Down\n")
}

func (det *Detector) spawnStatsThread() {
	for {
		det.Logger.Println(det.stats.Report())
		det.stats.Reset()

		if det.exit {
			return
		}
		time.Sleep(time.Duration(det.StatsFrequency) * time.Second)
	}
}

func (det *Detector) handlePacket(packet gopacket.Packet) {
	dst := packet.NetworkLayer().NetworkFlow().Dst()
	src := packet.NetworkLayer().NetworkFlow().Src()
	var dstPort uint16

	det.stats.BytesTotal += uint64(packet.Metadata().CaptureLength)
	switch len(dst.Raw()) {
	case 4:
		det.stats.V4PacketCount++
	case 16:
		det.stats.V6PacketCount++
	default:
		det.Logger.Warn("IP is not valid as IPv4 or IPv6")
	}

	if tcpLayer := packet.Layer(layers.LayerTypeTCP); tcpLayer != nil {
		tcp, _ := tcpLayer.(*layers.TCP)
		dstPort = uint16(tcp.DstPort)
	} else {
		return
	}

	det.checkForTags(packet)

	if det.IsRegistered(dst.String(), src.String(), dstPort) {
		det.forwardPacket(packet)
	}
}

// Current stations check packets for tags (UDP specifically to check DNS)
// TODO
func (det *Detector) checkForTags(packet gopacket.Packet) {
	for _, tag := range det.Tags {
		if bytes.Contains(packet.ApplicationLayer().Payload(), []byte(tag)) {
			dst := packet.NetworkLayer().NetworkFlow().Dst()
			src := packet.NetworkLayer().NetworkFlow().Src()
			det.Logger.Println("confirmed", src, "->", dst)
		}
	}
}

// Connect tot the tun interface and send the packet to the other portion of
// the refraction station. TODO
func (det *Detector) forwardPacket(packet gopacket.Packet) {
	dst := packet.NetworkLayer().NetworkFlow().Dst()
	src := packet.NetworkLayer().NetworkFlow().Src()
	det.Logger.Println(src, "->", dst)
}

func generateFilters(filterList []string) string {

	if len(filterList) == 0 {
		return ""
	}

	out := "tcp and not src " + filterList[0]
	for _, entry := range filterList[1:] {
		out += " and not src " + entry
	}

	return out
}

func deviceExists(name string) bool {
	devices, err := pcap.FindAllDevs()
	if err != nil {
		log.Panic(err)
	}

	for _, device := range devices {
		if device.Name == name {
			return true
		}
	}
	return false
}

var (
	iface  = "wlp4s0"
	buffer = int32(1600)
	filter = "tcp and port 22 and not src 192.168.1.104"
)

func main() {

	det := &Detector{
		Iface:      iface,
		FilterList: []string{"192.168.1.104"},
		IsRegistered: func(src, dst string, dstPort uint16) bool {
			return true
		},
		Logger:         logrus.New(),
		stats:          &DetectorStats{},
		StatsFrequency: 3,
	}

	det.Run()
}
