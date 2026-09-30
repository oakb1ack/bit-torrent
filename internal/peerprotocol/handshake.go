package peerprotocol

import(
	"fmt"
)

type Handshake struct {
	ProtocolStringLength byte
	ProtocolString       string
	Reserved             [8]byte
	InfoHash             [20]byte
	PeerID               [20]byte
}


func EncodeHandshake(handshake Handshake) ([]byte, error) {
	var ans []byte

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

func DecodeHandshake(handshake []byte) (Handshake, error) {

}
