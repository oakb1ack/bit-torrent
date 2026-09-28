package tracker

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"

	"github.com/oakb1ack/bit-torrent/internal/bencode"
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

	decoded, err := bencode.Decode(string(bodyBytes))
	if err != nil {
		return nil, err
	}

	dict, ok := decoded.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("tracker response is not a dictionary")
	}

	peersValue, ok := dict["peers"].(string)
	if !ok {
		return nil, fmt.Errorf("tracker response does not contain a valid peers string")
	}
	rawPeers := []byte(peersValue)

	if len(rawPeers)%6 != 0 {
		return nil, fmt.Errorf("Length of rawPeers not divisible by 6")
	}

	peers := make([]Peer, 0, len(rawPeers)/6)
	for i := 0; i < len(rawPeers); i += 6 {
		ip := net.IP(rawPeers[i : i+4])
		port := binary.BigEndian.Uint16(rawPeers[i+4 : i + 6])
		peers = append(peers, Peer{IP: ip, Port: port})
	}

	return peers, nil
}
