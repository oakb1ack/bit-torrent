package peerprotocol

import(
	"fmt"
)

const bitTorrentProtocol = "BitTorrent protocol"

type Handshake struct {
	ProtocolStringLength byte
	ProtocolString       string
	Reserved             [8]byte
	InfoHash             [20]byte
	PeerID               [20]byte
}


func EncodeHandshake(handshake Handshake) ([]byte, error) {
	var ans []byte

	if handshake.ProtocolString != bitTorrentProtocol {
		return nil, fmt.Errorf("unsupported protocol string")
	}

	if len(handshake.ProtocolString) > 255 ||  byte(len(handshake.ProtocolString)) != handshake.ProtocolStringLength{
		return nil, fmt.Errorf("protocol string length not properly encoded")
	}

	ans = append(ans, handshake.ProtocolStringLength)

	for _, value := range []byte(handshake.ProtocolString) {
		ans = append(ans, value)
	} 

	for _, value := range (handshake.Reserved) {
		ans = append(ans, value)
	} 

	for _, value := range (handshake.InfoHash) {
		ans = append(ans, value)
	} 

	for _, value := range (handshake.PeerID) {
		ans = append(ans, value)
	} 

	return ans, nil
}

func DecodeHandshake(ans []byte) (Handshake, error) {
	
	var handshake Handshake

	if len(ans) == 0 {
		return handshake, fmt.Errorf("no valid input was detected")
	}
	
	handshake.ProtocolStringLength = ans[0]

	if len(ans) != int(handshake.ProtocolStringLength) + 49 {
		return handshake, fmt.Errorf("input length invalid")
	} 
	
	var endOfString int = int(handshake.ProtocolStringLength) + 1

	handshake.ProtocolString = string(ans[1:endOfString])
	if handshake.ProtocolString != bitTorrentProtocol {
		return handshake, fmt.Errorf("unsupported protocol string")
	}
	copy(handshake.Reserved[:], ans[endOfString: endOfString + 8])
	copy(handshake.InfoHash[:], ans[endOfString+8: endOfString + 28])
	copy(handshake.PeerID[:], ans[endOfString+28: endOfString + 48])

	return handshake, nil
}
