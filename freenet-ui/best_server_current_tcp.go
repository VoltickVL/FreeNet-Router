package main

import (
	"context"
	"net"
	"strconv"
)

func enrichCurrentBestServerTCP(ctx context.Context, response *bestServerQualityResponse, endpoint string) {
	if response == nil || endpoint == "" {
		return
	}
	host, portText, err := net.SplitHostPort(endpoint)
	if err != nil || host == "" {
		return
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		return
	}
	probe := defaultBestServerQualityTCPProbe(ctx, subscriptionProfile{Address: host, Port: port})
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
