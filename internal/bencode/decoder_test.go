package bencode

import (
	"reflect"
	"testing"
)

func TestDecoder(t *testing.T) {
	tests := []struct {
		name             string
		input            string
		position         int
		expectedValue    any
		expectedPosition int
		expectError      bool
	}{
		// -------------------------------------------------
		// Integers
		// -------------------------------------------------
		{
			name:             "positive integer",
			input:            "i42e",
			position:         0,
			expectedValue:    42,
			expectedPosition: 4,
		},
		{
			name:             "negative integer",
			input:            "i-42e",
			position:         0,
			expectedValue:    -42,
			expectedPosition: 5,
		},
		{
			name:             "zero integer",
			input:            "i0e",
			position:         0,
			expectedValue:    0,
			expectedPosition: 3,
		},
		{
			name:        "integer missing terminator",
			input:       "i42",
			position:    0,
			expectError: true,
		},
		{
			name:        "integer contains letters",
			input:       "iabce",
			position:    0,
			expectError: true,
		},
		{
			name:        "empty integer",
			input:       "ie",
			position:    0,
			expectError: true,
		},

		// -------------------------------------------------
		// Strings
		// -------------------------------------------------
		{
			name:             "basic string",
			input:            "5:hello",
			position:         0,
			expectedValue:    "hello",
			expectedPosition: 7,
		},
		{
			name:             "empty string",
			input:            "0:",
			position:         0,
			expectedValue:    "",
			expectedPosition: 2,
		},
		{
			name:             "string containing spaces",
			input:            "11:hello world",
			position:         0,
			expectedValue:    "hello world",
			expectedPosition: 14,
		},
		{
			name:        "string missing colon",
			input:       "5hello",
			position:    0,
			expectError: true,
		},
		{
			name:        "string claims too many bytes",
			input:       "10:hello",
			position:    0,
			expectError: true,
		},
		{
			name:        "length only",
			input:       "5",
			position:    0,
			expectError: true,
		},

		// -------------------------------------------------
		// Lists
		// -------------------------------------------------
		{
			name:             "empty list",
			input:            "le",
			position:         0,
			expectedValue:    []any{},
			expectedPosition: 2,
		},
		{
			name:             "list with one integer",
			input:            "li1ee",
			position:         0,
			expectedValue:    []any{1},
			expectedPosition: 5,
		},
		{
			name:             "list with multiple integers",
			input:            "li1ei2ei3ee",
			position:         0,
			expectedValue:    []any{1, 2, 3},
			expectedPosition: 11,
		},
		{
			name:             "list with strings",
			input:            "l3:cat4:spame",
			position:         0,
			expectedValue:    []any{"cat", "spam"},
			expectedPosition: 13,
		},
		{
			name:             "mixed list",
			input:            "li42e5:helloe",
			position:         0,
			expectedValue:    []any{42, "hello"},
			expectedPosition: 13,
		},
		{
			name:             "nested list",
			input:            "lli1eee",
			position:         0,
			expectedValue:    []any{[]any{1}},
			expectedPosition: 7,
		},
		{
			name:        "list missing terminator",
			input:       "li1e",
			position:    0,
			expectError: true,
		},
		{
			name:        "list contains malformed value",
			input:       "liabcee",
			position:    0,
			expectError: true,
		},

		// -------------------------------------------------
		// Dictionaries
		// -------------------------------------------------
		{
			name:             "empty dictionary",
			input:            "de",
			position:         0,
			expectedValue:    map[string]any{},
			expectedPosition: 2,
		},
		{
			name:             "dictionary string value",
			input:            "d3:foo3:bare",
			position:         0,
			expectedValue: map[string]any{
				"foo": "bar",
			},
			expectedPosition: 12,
		},
		{
			name:             "dictionary integer value",
			input:            "d3:numi42ee",
			position:         0,
			expectedValue: map[string]any{
				"num": 42,
			},
			expectedPosition: 11,
		},
		{
			name:             "dictionary with multiple entries",
			input:            "d3:agei5e4:name3:cate",
			position:         0,
			expectedValue: map[string]any{
				"age":  5,
				"name": "cat",
			},
			expectedPosition: 21,
		},
		{
			name:             "dictionary containing list",
			input:            "d4:listli1ei2eee",
			position:         0,
			expectedValue: map[string]any{
				"list": []any{1, 2},
			},
			expectedPosition: 16,
		},
		{
			name:             "nested dictionary",
			input:            "d5:outerd3:key5:valueee",
			position:         0,
			expectedValue: map[string]any{
				"outer": map[string]any{
					"key": "value",
				},
			},
			expectedPosition: 23,
		},
		{
			name:        "dictionary key is not string",
			input:       "di1e3:fooe",
			position:    0,
			expectError: true,
		},
		{
			name:        "dictionary key missing value",
			input:       "d3:fooe",
			position:    0,
			expectError: true,
		},
		{
			name:        "dictionary missing terminator",
			input:       "d3:foo3:bar",
			position:    0,
			expectError: true,
		},

		// -------------------------------------------------
		// Position / cursor behavior
		// -------------------------------------------------
		{
			name:             "decode integer from middle of input",
			input:            "XXXi42eYYY",
			position:         3,
			expectedValue:    42,
			expectedPosition: 7,
		},
		{
			name:             "decode string from middle of input",
			input:            "XXX3:catYYY",
			position:         3,
			expectedValue:    "cat",
			expectedPosition: 8,
		},

		// -------------------------------------------------
		// General malformed input
		// -------------------------------------------------
		{
			name:        "empty input",
			input:       "",
			position:    0,
			expectError: true,
		},
		{
			name:        "position at end of input",
			input:       "i1e",
			position:    3,
			expectError: true,
		},
		{
			name:        "position beyond end of input",
			input:       "i1e",
			position:    20,
			expectError: true,
		},
		{
			name:        "unknown bencode type",
			input:       "x123",
			position:    0,
			expectError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, position, err := decoder(test.input, test.position)

			if test.expectError {
				if err == nil {
					t.Fatalf(
						"decoder(%q, %d) expected an error, got value=%v position=%d",
						test.input,
						test.position,
						value,
						position,
					)
				}

				return
			}

			if err != nil {
				t.Fatalf(
					"decoder(%q, %d) returned unexpected error: %v",
					test.input,
					test.position,
					err,
				)
			}

			if !reflect.DeepEqual(value, test.expectedValue) {
				t.Errorf(
					"decoder(%q, %d) value = %#v, expected %#v",
					test.input,
					test.position,
					value,
					test.expectedValue,
				)
			}

			if position != test.expectedPosition {
				t.Errorf(
					"decoder(%q, %d) position = %d, expected %d",
					test.input,
					test.position,
					position,
					test.expectedPosition,
				)
			}
		})
	}
}
