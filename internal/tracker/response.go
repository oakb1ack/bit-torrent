package tracker

import (
	"fmt"
	"net/http"
	"net/url"
	"net/ip"
)

type Peer struct {
	IP net.IP
	Port uint16
}

func RequestPeers(link string, params Parameters) ([]Peer, error) {
}
