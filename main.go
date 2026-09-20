package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"

	"github.com/oakb1ack/bit-torrent/internal/bencode"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Println("please include torrent source")
		return
	}

	source := os.Args[1]
	parsed, err := url.Parse(source)
	if err != nil {
		fmt.Println("error parsing source:", err)
		return
	}

	var data []byte

	if parsed.Scheme == "http" || parsed.Scheme == "https" {
		resp, err := http.Get(source)
		if err != nil {
			fmt.Println("dns/connection issue:", err)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			fmt.Println("server returned status:", resp.StatusCode)
			return
		}

		data, err = io.ReadAll(resp.Body)
		if err != nil {
			fmt.Println("error reading response:", err)
			return
		}
	} else {
		data, err = os.ReadFile(source)
		if err != nil {
			fmt.Println("error reading file:", err)
			return
		}
	}

	ans, err := bencode.Decode(string(data))
	if err != nil {
		fmt.Println("error decoding torrent:", err)
		return
	}

	torrent, ok := ans.(map[string]any)
	if !ok {
		fmt.Println("torrent root is not a dictionary")
		return
	}

	announce, ok := torrent["announce"].(string)
	if !ok {
		fmt.Println("torrent does not contain a valid announce URL")
		return
	}

	info, ok := torrent["info"].(map[string]any)
	if !ok {
		fmt.Println("torrent does not contain a valid info dictionary")
		return
	}

	fmt.Println("Announce:", announce)
	fmt.Println("Name:", info["name"].(string))
	fmt.Println("Length:", info["length"].(int))
	fmt.Println("Piece Length:", info["piece length"].(int))
}
