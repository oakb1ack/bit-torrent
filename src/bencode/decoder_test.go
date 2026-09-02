package bencode

import "testing"

func TestDecoder(t *testing.T) {
	tests := []struct {
		input        string
		expected     any
		expectedPos  int
		shouldError  bool
	}{
		{"i42e", 42, 4, false},
		{"i-42e", -42, 5, false},
		{"5:hello", "hello", 7, false},
		{"4:spam", "spam", 6, false},
		{"0:", "", 2, false},

		{"i42", nil, 0, true},
		{"5hello", nil, 0, true},
		{"10:hello", nil, 0, true},
	}

	for _, test := range tests {
		value, pos, err := decoder(test.input, 0)

		if test.shouldError {
			if err == nil {
				t.Errorf("decoder(%q) expected error, got nil", test.input)
			}
			continue
		}

		if err != nil {
			t.Errorf("decoder(%q) unexpected error: %v", test.input, err)
			continue
		}

		if value != test.expected {
			t.Errorf(
				"decoder(%q) value = %v, expected %v",
				test.input,
				value,
				test.expected,
			)
		}

		if pos != test.expectedPos {
			t.Errorf(
				"decoder(%q) position = %d, expected %d",
				test.input,
				pos,
				test.expectedPos,
			)
		}
	}
}
