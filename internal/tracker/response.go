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
	IP   net.IP
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
	if resp == nil {
		return nil, fmt.Errorf("tracker returned no response")
	}
	if resp.Body == nil {
		return nil, fmt.Errorf("tracker response has no body")
	}
	// A zero status is useful for an in-memory response supplied by callers;
	// real net/http responses always carry a status code.
	if resp.StatusCode != 0 && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("tracker returned HTTP %s: %s", resp.Status, string(body))
	}
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
	if failure, ok := dict["failure reason"].(string); ok {
		return nil, fmt.Errorf("tracker failure: %s", failure)
	}

	peersValue, ok := dict["peers"].(string)
	if !ok {
		return nil, fmt.Errorf("tracker response does not contain a valid peers string")
	}
	rawPeers := []byte(peersValue)

	if len(rawPeers)%6 != 0 {
		return nil, fmt.Errorf("compact peer data length %d is not divisible by 6", len(rawPeers))
	}

	peers := make([]Peer, 0, len(rawPeers)/6)
	for i := 0; i < len(rawPeers); i += 6 {
		ip := net.IP(rawPeers[i : i+4])
		port := binary.BigEndian.Uint16(rawPeers[i+4 : i+6])
		peers = append(peers, Peer{IP: ip, Port: port})
	}

	return peers, nil
}
