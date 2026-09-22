package tracker

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Parameters struct {
	InfoHash   [20]byte
	PeerID     [20]byte
	Port       int
	Uploaded   int64
	Downloaded int64
	Left       int64
	Compact    int32
}

func RequestPeers(link string, params Parameters) (*http.Response, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}

	query := u.Query()

	query.Set("info_hash", string(params.InfoHash[:]))
	query.Set("peer_id", string(params.PeerID[:]))
	query.Set("port", strconv.Itoa(params.Port))
	query.Set("uploaded", strconv.FormatInt(params.Uploaded, 10))
	query.Set("downloaded", strconv.FormatInt(params.Downloaded, 10))
	query.Set("left", strconv.FormatInt(params.Left, 10))
	query.Set("compact", strconv.FormatInt(int64(params.Compact), 10))

	u.RawQuery = query.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	fmt.Println(req.URL.String())
	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	return client.Do(req)
}
