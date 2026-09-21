package tracker

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

type parameters struct {
	infoHash   [20]byte
	peerID     [20]byte
	port       int
	uploaded   int
	downloaded int
	left       int
	compact    int
}

func buildRequest(link string, params parameters) (*http.Response, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}

	query := u.Query()
	
	query.Set("into_hash", string(params.infoHash[:]))
	query.Set("peer_id", string(params.peerID[:]))
	query.Set("port", strconv.Itoa(params.port))
	query.Set("uploaded", strconv.Itoa(params.downloaded))
	query.Set("downloaded", strconv.Itoa(params.uploaded))
	query.Set("left", strconv.Itoa(params.left))
	query.Set("compact", strconv.Itoa(params.compact))

	u.RawQuery = query.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	fmt.Println(req.URL.String())
	client := &http.Client{}

	return client.Do(req)
}
