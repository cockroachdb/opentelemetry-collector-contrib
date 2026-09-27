// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package tcp // import "github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/operator/input/tcp"

import (
	"encoding/json"
	"unicode/utf8"
)

const (
	// truncatedJSONMarker ends a string value that was cut by truncation.
	truncatedJSONMarker = "…"

	// maxTruncatedJSONDepth bounds the nesting a repair will close. Deeper
	// input falls back to a plain prefix, which also keeps the closing
	// brackets inside the reserved budget below.
	maxTruncatedJSONDepth = 64

	// truncatedJSONReserve is the space held back from max_log_size so the
	// marker, a closing quote and every closing bracket always fit.
	truncatedJSONReserve = len(truncatedJSONMarker) + 1 + maxTruncatedJSONDepth
)

// JSON container frame states.
const (
	objectKeyOrEnd   = iota // after '{'
	objectKey               // after ','
	objectColon             // after a key
	objectValue             // after ':'
	objectCommaOrEnd        // after a value
	arrayValueOrEnd         // after '['
	arrayValue              // after ','
	arrayCommaOrEnd         // after a value
)

type jsonFrame struct {
	object bool
	state  int
}

// repairTruncatedJSON turns data, a prefix of a JSON object cut at an
// arbitrary byte, into a valid JSON object of at most maxLen bytes that keeps
// every value completed before the cut. A string value cut mid-way is kept up
// to the cut and ends in truncatedJSONMarker; a cut number, literal or key is
// dropped along with the member it belongs to. It returns false, and the
// caller should keep the plain prefix, when data is not a JSON object prefix
// or cannot be repaired safely.
func repairTruncatedJSON(data []byte, maxLen int) ([]byte, bool) {
	limit := min(len(data), maxLen-truncatedJSONReserve)
	if limit <= 0 {
		return nil, false
	}
	data = data[:limit]

	i := skipJSONSpace(data, 0)
	if i >= len(data) || data[i] != '{' {
		return nil, false
	}

	var (
		stack []jsonFrame
		// safeEnd/safeClose describe the latest point at which the prefix
		// plus closing brackets is complete JSON.
		safeEnd   int
		safeClose []byte
	)
	markSafe := func(end int) {
		safeEnd = end
		safeClose = closingBrackets(stack, safeClose[:0])
	}
	// valueDone advances the enclosing frame after a complete value and
	// records the position as safe.
	valueDone := func(end int) bool {
		if len(stack) == 0 {
			// The root object closed before the cut: the input was not a
			// single truncated object.
			return false
		}
		top := &stack[len(stack)-1]
		if top.object {
			top.state = objectCommaOrEnd
		} else {
			top.state = arrayCommaOrEnd
		}
		markSafe(end)
		return true
	}

	for i < len(data) {
		c := data[i]
		switch c {
		case ' ', '\t', '\n', '\r':
			i++
			continue
		}

		var top *jsonFrame
		if len(stack) > 0 {
			top = &stack[len(stack)-1]
		}

		switch {
		case top == nil:
			// Only the root '{' is accepted at depth zero.
			stack = append(stack, jsonFrame{object: true, state: objectKeyOrEnd})
			i++
			markSafe(i)

		case top.object && (top.state == objectKeyOrEnd || top.state == objectKey):
			if c == '}' && top.state == objectKeyOrEnd {
				stack = stack[:len(stack)-1]
				i++
				if !valueDone(i) {
					return nil, false
				}
				continue
			}
			if c != '"' {
				return nil, false
			}
			end, ok := scanJSONString(data, i)
			if !ok {
				// Cut inside a key: the member is incomplete.
				return finishTruncatedJSON(data, safeEnd, safeClose)
			}
			top.state = objectColon
			i = end

		case top.object && top.state == objectColon:
			if c != ':' {
				return nil, false
			}
			top.state = objectValue
			i++

		case top.object && top.state == objectCommaOrEnd,
			!top.object && top.state == arrayCommaOrEnd:
			switch {
			case c == ',':
				if top.object {
					top.state = objectKey
				} else {
					top.state = arrayValue
				}
				i++
			case c == '}' && top.object, c == ']' && !top.object:
				stack = stack[:len(stack)-1]
				i++
				if !valueDone(i) {
					return nil, false
				}
			default:
				return nil, false
			}

		default:
			// A value position: object value, or array element.
			if c == ']' && !top.object && top.state == arrayValueOrEnd {
				stack = stack[:len(stack)-1]
				i++
				if !valueDone(i) {
					return nil, false
				}
				continue
			}
			switch c {
			case '{', '[':
				if len(stack) >= maxTruncatedJSONDepth {
					return nil, false
				}
				if c == '{' {
					stack = append(stack, jsonFrame{object: true, state: objectKeyOrEnd})
				} else {
					stack = append(stack, jsonFrame{object: false, state: arrayValueOrEnd})
				}
				i++
				markSafe(i)
			case '"':
				end, ok := scanJSONString(data, i)
				if !ok {
					// Cut inside a string value: keep what fits.
					return finishTruncatedString(data, i, stack)
				}
				i = end
				if !valueDone(i) {
					return nil, false
				}
			default:
				end, ok := scanJSONScalar(data, i)
				if !ok {
					// Cut inside a number or literal, or malformed input:
					// a scalar is never emitted partially.
					if end == len(data) {
						return finishTruncatedJSON(data, safeEnd, safeClose)
					}
					return nil, false
				}
				i = end
				if !valueDone(i) {
					return nil, false
				}
			}
		}
	}
	return finishTruncatedJSON(data, safeEnd, safeClose)
}

// finishTruncatedJSON closes the prefix at the last safe point.
func finishTruncatedJSON(data []byte, end int, closers []byte) ([]byte, bool) {
	if end == 0 {
		return nil, false
	}
	out := make([]byte, 0, end+len(closers))
	out = append(out, data[:end]...)
	out = append(out, closers...)
	if !json.Valid(out) {
		return nil, false
	}
	return out, true
}

// finishTruncatedString keeps a string value cut by truncation, backing off to
// the last complete character and escape, then closes it and its containers.
func finishTruncatedString(data []byte, start int, stack []jsonFrame) ([]byte, bool) {
	end := lastCompleteStringEnd(data, start)
	closers := closingBrackets(stack, nil)
	out := make([]byte, 0, end+len(truncatedJSONMarker)+1+len(closers))
	out = append(out, data[:end]...)
	out = append(out, truncatedJSONMarker...)
	out = append(out, '"')
	out = append(out, closers...)
	if !json.Valid(out) {
		return nil, false
	}
	return out, true
}

// lastCompleteStringEnd returns the largest position within the unterminated
// string starting at data[start] (its opening quote) that does not split an
// escape sequence or a UTF-8 character.
func lastCompleteStringEnd(data []byte, start int) int {
	end := start + 1
	for i := start + 1; i < len(data); {
		if data[i] == '\\' {
			n := 2
			if i+1 < len(data) && data[i+1] == 'u' {
				n = 6
			}
			if i+n > len(data) {
				break
			}
			i += n
			end = i
			continue
		}
		r, size := utf8.DecodeRune(data[i:])
		if r == utf8.RuneError && size <= 1 && !utf8.FullRune(data[i:]) {
			break
		}
		i += size
		end = i
	}
	return end
}

// scanJSONString returns the position just past the string starting at
// data[start], or false if the data ends before the string does.
func scanJSONString(data []byte, start int) (int, bool) {
	for i := start + 1; i < len(data); i++ {
		switch data[i] {
		case '\\':
			i++
		case '"':
			return i + 1, true
		}
	}
	return len(data), false
}

// scanJSONScalar scans a number or literal. It succeeds only when a delimiter
// follows, so a scalar at the very end of data (which may continue past the
// cut) is reported as incomplete with end == len(data).
func scanJSONScalar(data []byte, start int) (int, bool) {
	i := start
	for i < len(data) {
		switch c := data[i]; {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c == '-', c == '+', c == '.', c == 'E':
			i++
			continue
		}
		break
	}
	if i == len(data) {
		return i, false
	}
	if i == start {
		return i, false
	}
	switch data[i] {
	case ',', '}', ']', ' ', '\t', '\n', '\r':
	default:
		return i, false
	}
	token := data[start:i]
	switch string(token) {
	case "true", "false", "null":
		return i, true
	}
	if token[0] == '-' || (token[0] >= '0' && token[0] <= '9') {
		return i, json.Valid(token)
	}
	return i, false
}

func skipJSONSpace(data []byte, i int) int {
	for i < len(data) {
		switch data[i] {
		case ' ', '\t', '\n', '\r':
			i++
		default:
			return i
		}
	}
	return i
}

func closingBrackets(stack []jsonFrame, dst []byte) []byte {
	for j := len(stack) - 1; j >= 0; j-- {
		if stack[j].object {
			dst = append(dst, '}')
		} else {
			dst = append(dst, ']')
		}
	}
	return dst
}
