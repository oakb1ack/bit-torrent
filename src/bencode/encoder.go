package bencode

import (
	"fmt"
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

		v_list = []string
		for key, _ in v{
			v_list = append(v_list, key)
		}
		sort.String(v_list)
	default:
		return "", fmt.Errorf("unsupported type")
	}
}
