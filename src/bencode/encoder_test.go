package bencode

import "testing"

func TestEncoder(t *testing.T) {
	tests := []struct {
		name        string
		input       any
		expected    string
		expectError bool
	}{
		// -------------------------------------------------
		// Integers
		// -------------------------------------------------
		{
			name:     "positive integer",
			input:    42,
			expected: "i42e",
		},
		{
			name:     "negative integer",
			input:    -42,
			expected: "i-42e",
		},
		{
			name:     "zero integer",
			input:    0,
			expected: "i0e",
		},
		{
			name:     "large integer",
			input:    123456789,
			expected: "i123456789e",
		},

		// -------------------------------------------------
		// Strings
		// -------------------------------------------------
		{
			name:     "basic string",
			input:    "hello",
			expected: "5:hello",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "0:",
		},
		{
			name:     "string with spaces",
			input:    "hello world",
			expected: "11:hello world",
		},
		{
			name:     "string containing colon",
			input:    "foo:bar",
			expected: "7:foo:bar",
		},
		{
			name:     "numeric string",
			input:    "12345",
			expected: "5:12345",
		},

		// -------------------------------------------------
		// Lists
		// -------------------------------------------------
		{
			name:     "empty list",
			input:    []any{},
			expected: "le",
		},
		{
			name:     "list with one integer",
			input:    []any{1},
			expected: "li1ee",
		},
		{
			name:     "list with multiple integers",
			input:    []any{1, 2, 3},
			expected: "li1ei2ei3ee",
		},
		{
			name:     "list with strings",
			input:    []any{"cat", "spam"},
			expected: "l3:cat4:spame",
		},
		{
			name:     "mixed list",
			input:    []any{42, "hello"},
			expected: "li42e5:helloe",
		},
		{
			name: "nested list",
			input: []any{
				[]any{1},
			},
			expected: "lli1eee",
		},
		{
			name: "deeply nested list",
			input: []any{
				1,
				[]any{
					2,
					[]any{
						3,
					},
				},
			},
			expected: "li1eli2eli3eeee",
		},

		// -------------------------------------------------
		// Dictionaries
		// -------------------------------------------------
		{
			name:     "empty dictionary",
			input:    map[string]any{},
			expected: "de",
		},
		{
			name: "dictionary with string value",
			input: map[string]any{
				"foo": "bar",
			},
			expected: "d3:foo3:bare",
		},
		{
			name: "dictionary with integer value",
			input: map[string]any{
				"num": 42,
			},
			expected: "d3:numi42ee",
		},
		{
			name: "dictionary with multiple entries",
			input: map[string]any{
				"name": "cat",
				"age":  5,
			},
			expected: "d3:agei5e4:name3:cate",
		},

		// This specifically verifies that your encoder sorts
		// dictionary keys instead of relying on Go map order.
		{
			name: "dictionary keys sorted lexicographically",
			input: map[string]any{
				"zebra": "z",
				"apple": "a",
				"cat":   "c",
			},
			expected: "d5:apple1:a3:cat1:c5:zebra1:ze",
		},

		{
			name: "uppercase sorts before lowercase",
			input: map[string]any{
				"website:": "codingchallenges.fyi",
				"Rating":   "Awesome",
			},
			expected: "d6:Rating7:Awesome8:website:20:codingchallenges.fyie",
		},

		{
			name: "dictionary containing list",
			input: map[string]any{
				"list": []any{1, 2},
			},
			expected: "d4:listli1ei2eee",
		},

		{
			name: "nested dictionary",
			input: map[string]any{
				"outer": map[string]any{
					"key": "value",
				},
			},
			expected: "d5:outerd3:key5:valueee",
		},

		{
			name: "dictionary with mixed nested types",
			input: map[string]any{
				"age": 42,
				"data": []any{
					"cat",
					7,
				},
				"name": "Bob",
			},
			expected: "d3:agei42e4:datal3:cati7ee4:name3:Bobe",
		},

		// -------------------------------------------------
		// Unsupported types
		// -------------------------------------------------
		{
			name:        "unsupported boolean",
			input:       true,
			expectError: true,
		},
		{
			name:        "unsupported float",
			input:       3.14,
			expectError: true,
		},
		{
			name:        "unsupported nil",
			input:       nil,
			expectError: true,
		},
		{
			name:        "unsupported int slice",
			input:       []int{1, 2, 3},
			expectError: true,
		},

		// -------------------------------------------------
		// Errors inside recursive containers
		// -------------------------------------------------
		{
			name: "unsupported value inside list",
			input: []any{
				1,
				true,
				3,
			},
			expectError: true,
		},
		{
			name: "unsupported value inside dictionary",
			input: map[string]any{
				"bad": true,
			},
			expectError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := encoder(test.input)

			if test.expectError {
				if err == nil {
					t.Fatalf(
						"encoder(%#v) expected error, got result %q",
						test.input,
						result,
					)
				}

				return
			}

			if err != nil {
				t.Fatalf(
					"encoder(%#v) returned unexpected error: %v",
					test.input,
					err,
				)
			}

			if result != test.expected {
				t.Errorf(
					"encoder(%#v) = %q, expected %q",
					test.input,
					result,
					test.expected,
				)
			}
		})
	}
}
