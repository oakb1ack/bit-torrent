package tracker

import (
	"fmt"
	"net/http"
	"net/url"
)

type Peer struct {
	IP net.IP
	Port uint16
}

func ShowPeers(link string, params Parameters) ([]Peer, error) {
	resp, err := RequestPeers(link, params)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	return DecodePeers(resp)
}

func DecodePeers(resp *http.Response) ([]Peer, error) {
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	decoded, err := Decode(string(bodyBytes))
	if err != nil {
		return nil, err
	}

	dict, err := decoded.(map[string]any)
	if err != nil {
		return nil, err
	}

	rawPeers, err := dict["peers"].([]byte)
	if err != nil {
		return nil, err
	}

	if len(rawPeers)%6 != 0 {
		return nil, fmt.Errorf("Length of rawPeers not divisible by 6")
	}

	peers = make([]peer, 0, len(rawPeers)/6)
	for i := 0; i < len(rawPeers); i+=6 {
		ip := net.IP(rawPeers[i : i+4])
		port := binary.BigEndian.Uint16(rawPeers[i+4 : i + 6])
		peers = append(peers, Peer{IP: ip, Port: port})
	}

	return peers, nil
}
