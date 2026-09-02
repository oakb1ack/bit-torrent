package bencode

import (
	"fmt"
	"strconv"
)

func decoder(input string, position int) (any, int, error) {
	var inputLength int = len(input)

	if position >= inputLength {
		return nil, position, fmt.Errorf("out of bounds")
	}

	switch {
	case input[position] == 'i':
		nPosition := position + 1

		for nPosition < inputLength && input[nPosition] != 'e'  {
			nPosition += 1
		}

		if nPosition >= inputLength || input[nPosition] != 'e' {
			return nil, position, fmt.Errorf("incorrct input")
		}

		number, err := strconv.Atoi(input[position + 1 : nPosition])

		if err != nil {
			return nil, position, fmt.Errorf("malformed output")
		}

		return number, nPosition + 1, nil
	
	case input[position] >= '0' && input[position] <= '9':
		nPosition := position

		for nPosition < inputLength && input[nPosition] >= '0' && input[nPosition] <= '9' {
			nPosition += 1
		}

		if nPosition >= inputLength {
			return nil, position, fmt.Errorf("out of bounds")
		}
		if input[nPosition] != ':' {
			return nil, position, fmt.Errorf("expected colon")
		}

		num, err := strconv.Atoi(input[position:nPosition])

		if err != nil {
			return nil, nPosition, fmt.Errorf("malformed output")
		}
		if nPosition + 1 + num > inputLength {
			return nil, position, fmt.Errorf("string length exceeds input")
		}

		return input[nPosition + 1: nPosition + 1 + num], nPosition + num + 1, nil
	case input[position] == 'l':
		nPosition := position + 1
		ans := []any{}

		for nPosition < inputLength && input[nPosition] != 'e' {
			value, tPosition, err := decoder(input, nPosition)

			if err != nil {
				return nil, position, fmt.Errorf("malformed output")
			}
			nPosition = tPosition
			ans = append(ans, value)

		}

		if nPosition >= inputLength {
			return nil, position, fmt.Errorf("out of bounds")
		}
		if input[nPosition] != 'e' {
			return nil, position, fmt.Errorf("malformed input")
		}

		return ans, nPosition + 1, nil
	case input[position] == "d":

	default:
		return nil, position, fmt.Errorf("unsupported bencode type at position %d", position)
	}
}
