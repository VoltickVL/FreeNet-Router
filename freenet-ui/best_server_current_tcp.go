package main

import (
	"context"
	"net"
	"strconv"
)

func bestServerProfileFromEndpoint(endpoint string) (subscriptionProfile, bool) {
	host, portText, err := net.SplitHostPort(endpoint)
	if err != nil || host == "" {
		return subscriptionProfile{}, false
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return subscriptionProfile{}, false
	}
	return subscriptionProfile{Address: host, Port: port}, true
}

func enrichCurrentBestServerTCP(ctx context.Context, response *bestServerQualityResponse, endpoint string) {
	if response == nil {
		return
	}
	profile, ok := bestServerProfileFromEndpoint(endpoint)
	if !ok {
		return
	}
	probe := defaultBestServerQualityTCPProbe(ctx, profile)
	if !probe.OK {
		return
	}
	for i := range response.Candidates {
		if !response.Candidates[i].Current {
			continue
		}
		response.Candidates[i].Reachable = true
		response.Candidates[i].TCPRTTMS = probe.Median
		response.Candidates[i].TCPJitterMS = probe.Jitter
		return
	}
}
