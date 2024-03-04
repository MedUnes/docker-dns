package main

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"strings"

	"github.com/miekg/dns"
)

const TLD = "docker"
const PORT = "5335"

func handleDNSQuery(w dns.ResponseWriter, r *dns.Msg) {
	msg := dns.Msg{}
	msg.SetReply(r)
	domain := msg.Question[0].Name
	records, err := fetchDockerRecords()
	if err != nil {
		log.Fatalf("Failed to fetch Docker DNS records: %s\n ", err.Error())
	}

	switch r.Question[0].Qtype {
	case dns.TypeA:
		if ipList, ok := records[domain]; ok {
			for _, ip := range ipList {
				msg.Answer = append(msg.Answer, &dns.A{
					Hdr: dns.RR_Header{Name: domain, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
					A:   net.ParseIP(ip),
				})
			}
		}
	}

	w.WriteMsg(&msg)
}

func main() {
	dns.HandleFunc(".", handleDNSQuery)
	server := &dns.Server{Addr: fmt.Sprintf(":%s", PORT), Net: "udp"}

	log.Printf("Starting DNS server on port %s", PORT)
	err := server.ListenAndServe()
	defer server.Shutdown()
	if err != nil {
		log.Fatalf("Failed to start server: %s\n ", err.Error())
	}

}
func fetchDockerRecords() (map[string][]string, error) {
	var records = make(map[string][]string)
	containerNameList, err := finsAllContainerNames()
	if err != nil {
		return nil, fmt.Errorf("Error while getting list of container names %v\n", err)
	}
	for _, containerName := range containerNameList {
		ipList, err := getContainerIPs(containerName)
		if err != nil {
			return nil, fmt.Errorf("Error getting IP addresses for container '%s': %v\n", containerName, err)
		}

		if len(ipList) == 0 {
			return nil, fmt.Errorf("No IP addresses found for container '%s'. Ensure the container is running.\n", containerName)
		}
		for _, ip := range ipList {
			fqdn := fmt.Sprintf("%s.%s.", containerName, TLD)
			records[fqdn] = append(records[fqdn], ip)
		}
	}

	return records, nil

}

func getContainerIPs(containerName string) ([]string, error) {
	cmd := exec.Command("docker", "inspect", "-f", "{{range .NetworkSettings.Networks}} {{.IPAddress}}{{end}}", containerName)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker command error: %s, output: %s", err, string(output))
	}
	ipList := strings.Fields(string(output))
	return ipList, nil
}
func finsAllContainerNames() ([]string, error) {
	cmd := exec.Command("docker", "ps", "--format", "table {{.Names}}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("docker command error: %s, output: %v", err, string(output))
	}
	containerList := strings.Split(string(output), "\n")
	return containerList[1 : len(containerList)-1], nil
}
