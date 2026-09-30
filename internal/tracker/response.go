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

	switch peersValue := dict["peers"].(type) {
	case string:
		return decodeCompactPeers([]byte(peersValue))
	case []any:
		return decodeDictionaryPeers(peersValue)
	default:
		return nil, fmt.Errorf("tracker response peers field has unsupported type %T", dict["peers"])
	}
}

func decodeCompactPeers(rawPeers []byte) ([]Peer, error) {
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

func decodeDictionaryPeers(values []any) ([]Peer, error) {
	peers := make([]Peer, 0, len(values))
	for i, value := range values {
		peerData, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tracker peer %d is not a dictionary", i)
		}
		ipText, ok := peerData["ip"].(string)
		if !ok {
			return nil, fmt.Errorf("tracker peer %d has no valid IP address", i)
		}
		ip := net.ParseIP(ipText)
		if ip == nil {
			return nil, fmt.Errorf("tracker peer %d has invalid IP address %q", i, ipText)
		}
		port, ok := peerData["port"].(int)
		if !ok || port < 1 || port > 65535 {
			return nil, fmt.Errorf("tracker peer %d has invalid port", i)
		}
		peers = append(peers, Peer{IP: ip, Port: uint16(port)})
	}
	return peers, nil
}
