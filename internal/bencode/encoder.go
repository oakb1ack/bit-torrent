package bencode

import (
	"fmt"
	"sort"
	"strconv"
)

func encoder(data any) (string, error) {
	var ans string = ""

	switch v := data.(type) {
	case int:
		ans = "i" + strconv.Itoa(v) + "e"
		return ans, nil
	case string:
		ans = strconv.Itoa(len(v)) + ":" + v
		return ans, nil
	case []any:
		ans += "l"
		for _, value := range v {
			tans, err := encoder(value)

			if err != nil {
				return "", fmt.Errorf("failed to encode list element")
			}

			ans += tans
		}
		ans += "e"
		return ans, nil
	case map[string]any:
		ans += "d"

		v_list := []string{}
		for key := range v {
			v_list = append(v_list, key)
		}
		sort.Strings(v_list)

		for _, x := range v_list {
			tans, err := encoder(x)

			if err != nil {
				return "", fmt.Errorf("failed to encode key dict element")
			}

			lans, err := encoder(v[x])

			if err != nil {
				return "", fmt.Errorf("failed to encode value dict element")
			}

			ans += tans + lans
		}

		return ans + "e", nil

	default:
		return "", fmt.Errorf("unsupported type")
	}
}
