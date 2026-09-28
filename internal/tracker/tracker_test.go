package tracker

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestRequestPeersBuildsGETRequest(t *testing.T) {
	var infoHash [20]byte
	var peerID [20]byte
	for i := range infoHash {
		infoHash[i] = byte(i)
		peerID[i] = byte(255 - i)
	}

	params := Parameters{
		InfoHash:   infoHash,
		PeerID:     peerID,
		Port:       6881,
		Uploaded:   12,
		Downloaded: 34,
		Left:       56,
		Compact:    1,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("request method = %q, want %q", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/announce" {
			t.Errorf("request path = %q, want %q", r.URL.Path, "/announce")
		}

		query := r.URL.Query()
		want := map[string]string{
			"info_hash":  string(infoHash[:]),
			"peer_id":    string(peerID[:]),
			"port":       "6881",
			"uploaded":   "12",
			"downloaded": "34",
			"left":       "56",
			"compact":    "1",
			"key":        "kept",
		}
		for key, value := range want {
			if got := query.Get(key); got != value {
				t.Errorf("query parameter %q = %q, want %q", key, got, value)
			}
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	resp, err := RequestPeers(server.URL+"/announce?key=kept", params)
	if err != nil {
		t.Fatalf("RequestPeers returned error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("response status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestDecodePeers(t *testing.T) {
	compactPeers := []byte{
		127, 0, 0, 1, 0x1a, 0xe1, // 127.0.0.1:6881
		8, 8, 4, 4, 0xc9, 0xf5, // 8.8.4.4:51701
	}

	resp := trackerResponse(compactPeers)
	peers, err := DecodePeers(resp)
	if err != nil {
		t.Fatalf("DecodePeers returned error: %v", err)
	}

	want := []Peer{
		{IP: net.IPv4(127, 0, 0, 1), Port: 6881},
		{IP: net.IPv4(8, 8, 4, 4), Port: 51701},
	}
	if len(peers) != len(want) {
		t.Fatalf("DecodePeers returned %d peers, want %d", len(peers), len(want))
	}
	for i := range want {
		if !peers[i].IP.Equal(want[i].IP) {
			t.Errorf("peer %d IP = %v, want %v", i, peers[i].IP, want[i].IP)
		}
		if peers[i].Port != want[i].Port {
			t.Errorf("peer %d port = %d, want %d", i, peers[i].Port, want[i].Port)
		}
	}
}

func TestDecodePeersRejectsInvalidResponses(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "invalid bencode",
			body: "not bencoded",
		},
		{
			name: "missing peers field",
			body: "de",
		},
		{
			name: "peer data is not a string",
			body: "d5:peersi1ee",
		},
		{
			name: "peer data is not a multiple of six bytes",
			body: "d5:peers5:abcdee",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp := &http.Response{Body: io.NopCloser(strings.NewReader(test.body))}
			if _, err := DecodePeers(resp); err == nil {
				t.Fatal("DecodePeers succeeded, want an error")
			}
		})
	}
}

func TestShowPeers(t *testing.T) {
	compactPeers := []byte{203, 0, 113, 7, 0x1a, 0xe1}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("request method = %q, want %q", r.Method, http.MethodGet)
		}
		if got := r.URL.Query().Get("compact"); got != "1" {
			t.Errorf("compact query parameter = %q, want %q", got, "1")
		}
		_, _ = io.WriteString(w, bencodedPeers(compactPeers))
	}))
	defer server.Close()

	peers, err := ShowPeers(server.URL, Parameters{Compact: 1})
	if err != nil {
		t.Fatalf("ShowPeers returned error: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("ShowPeers returned %d peers, want 1", len(peers))
	}
	if want := net.IPv4(203, 0, 113, 7); !peers[0].IP.Equal(want) {
		t.Errorf("peer IP = %v, want %v", peers[0].IP, want)
	}
	if peers[0].Port != 6881 {
		t.Errorf("peer port = %d, want 6881", peers[0].Port)
	}
}

func trackerResponse(compactPeers []byte) *http.Response {
	return &http.Response{
		Body: io.NopCloser(strings.NewReader(bencodedPeers(compactPeers))),
	}
}

func bencodedPeers(compactPeers []byte) string {
	return "d5:peers" + strconv.Itoa(len(compactPeers)) + ":" + string(compactPeers) + "e"
}
