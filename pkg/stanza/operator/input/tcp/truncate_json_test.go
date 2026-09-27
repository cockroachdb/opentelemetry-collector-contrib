// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package tcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component/componenttest"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/entry"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/operator"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/testutil"
)

// crdbRecord follows CRDB's json-fluent field order for a structured event.
const crdbRecord = `{"tag":"cockroach.sql_exec","channel_numeric":9,"channel":"SQL_EXEC","timestamp":"1720000000.000000001","severity":"INFO","redactable":1,"tags":{"client":"‹×›"},"event":{"Timestamp":1720000000000000001,"EventType":"sampled_query","Statement":"INSERT INTO t VALUES (1, \"a\"), (2, \"b\")","Tag":"INSERT","User":"‹×›","PlaceholderValues":["x","y"],"NumRows":2,"FullTableScan":false,"ErrorText":null}}`

func repairAt(t *testing.T, record string, cut int) string {
	t.Helper()
	out, ok := repairTruncatedJSON([]byte(record[:cut]), cut+truncatedJSONReserve)
	require.True(t, ok, "cut=%d prefix=%q", cut, record[:cut])
	return string(out)
}

func TestRepairTruncatedJSON(t *testing.T) {
	cutAfter := func(marker string) int {
		i := strings.Index(crdbRecord, marker)
		require.GreaterOrEqual(t, i, 0, marker)
		return i + len(marker)
	}

	tests := []struct {
		name string
		cut  int
		want string
	}{
		{
			name: "inside a string value keeps the prefix with a marker",
			cut:  cutAfter(`"Statement":"INSERT INTO t`),
			want: `"Statement":"INSERT INTO t…"}}`,
		},
		{
			name: "inside an escape backs off before the backslash",
			cut:  cutAfter(`VALUES (1, \`),
			want: `"Statement":"INSERT INTO t VALUES (1, …"}}`,
		},
		{
			name: "inside a multi-byte character backs off to a whole character",
			cut:  cutAfter(`"User":"‹×`) - 1,
			want: `"User":"‹…"}}`,
		},
		{
			name: "inside a key drops the member",
			cut:  cutAfter(`"Us`),
			want: `"Tag":"INSERT"}}`,
		},
		{
			name: "between key and value drops the member",
			cut:  cutAfter(`"User":`),
			want: `"Tag":"INSERT"}}`,
		},
		{
			name: "after a comma drops the comma",
			cut:  cutAfter(`"Tag":"INSERT",`),
			want: `"Tag":"INSERT"}}`,
		},
		{
			name: "inside a number drops the member",
			cut:  cutAfter(`"NumRows":`) + 1,
			want: `"PlaceholderValues":["x","y"]}}`,
		},
		{
			name: "inside a literal drops the member",
			cut:  cutAfter(`"FullTableScan":fal`),
			want: `"NumRows":2}}`,
		},
		{
			name: "inside an array keeps complete elements",
			cut:  cutAfter(`"PlaceholderValues":["x",`),
			want: `"PlaceholderValues":["x"]}}`,
		},
		{
			name: "inside an array string keeps the partial element",
			cut:  cutAfter(`"PlaceholderValues":["x","`),
			want: `"PlaceholderValues":["x","…"]}}`,
		},
		{
			name: "right after an opening brace keeps an empty object",
			cut:  cutAfter(`"event":{`),
			want: `"event":{}}`,
		},
		{
			name: "complete nested object stays complete",
			cut:  cutAfter(`"tags":{"client":"‹×›"}`),
			want: `"tags":{"client":"‹×›"}}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := repairAt(t, crdbRecord, tt.cut)
			require.True(t, json.Valid([]byte(got)), got)
			require.True(t, strings.HasSuffix(got, tt.want), "got %q\nwant suffix %q", got, tt.want)
			requireTruncationOf(t, got, crdbRecord)
		})
	}
}

func TestRepairTruncatedJSONEveryCut(t *testing.T) {
	// Every cut point of a realistic record repairs into a faithful truncation.
	for cut := 1; cut < len(crdbRecord); cut++ {
		out, ok := repairTruncatedJSON([]byte(crdbRecord[:cut]), cut+truncatedJSONReserve)
		require.True(t, ok, "cut=%d prefix=%q", cut, crdbRecord[:cut])
		require.True(t, json.Valid(out), "cut=%d out=%q", cut, out)
		require.LessOrEqual(t, len(out), cut+truncatedJSONReserve)
		requireTruncationOf(t, string(out), crdbRecord)
	}
}

func TestRepairTruncatedJSONFallsBack(t *testing.T) {
	deep := strings.Repeat(`{"a":`, maxTruncatedJSONDepth+1) + `"x`
	tests := map[string]string{
		"not json":               `goroutine 1 [running]:` + strings.Repeat("x", 200),
		"array root":             `["a","b"` + strings.Repeat(`,"c"`, 50),
		"root closed before cut": `{"a":1} trailing text` + strings.Repeat("x", 200),
		"malformed":              `{"a":1,,"b":2` + strings.Repeat("x", 200),
		"too deep":               deep,
		"empty":                  ``,
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			_, ok := repairTruncatedJSON([]byte(input), len(input)+truncatedJSONReserve)
			require.False(t, ok)
		})
	}
}

func TestRepairTruncatedJSONRespectsMaxLen(t *testing.T) {
	record := `{"message":"` + strings.Repeat("y", 1000)
	for _, maxLen := range []int{truncatedJSONReserve + 20, 100, 500, 1000} {
		out, ok := repairTruncatedJSON([]byte(record), maxLen)
		require.True(t, ok)
		require.LessOrEqual(t, len(out), maxLen)
		require.True(t, json.Valid(out))
	}
	_, ok := repairTruncatedJSON([]byte(record), truncatedJSONReserve)
	require.False(t, ok, "no room left for content")
}

// FuzzRepairTruncatedJSON cuts valid JSON objects at arbitrary points. The
// repair must never panic, must stay within maxLen, and when it succeeds must
// produce valid JSON in which every value is either the original value or a
// marker-terminated prefix of an original string.
func FuzzRepairTruncatedJSON(f *testing.F) {
	f.Add(crdbRecord, uint16(40))
	f.Add(crdbRecord, uint16(200))
	f.Add(`{"a":"é😀","b":[1,2.5e3,-0.1,{"c":[]}],"d":true}`, uint16(15))
	f.Add(`{"s":"line\nnext\t\"quoted\" \\ back"}`, uint16(20))
	f.Fuzz(func(t *testing.T, record string, cut uint16) {
		if !json.Valid([]byte(record)) || !strings.HasPrefix(strings.TrimLeft(record, " \t\r\n"), "{") ||
			hasDuplicateKeys(record) || !utf8.ValidString(record) {
			t.Skip()
		}
		n := int(cut) % (len(record) + 1)
		maxLen := n + truncatedJSONReserve
		out, ok := repairTruncatedJSON([]byte(record[:n]), maxLen)
		if !ok {
			return
		}
		require.LessOrEqual(t, len(out), maxLen)
		require.True(t, json.Valid(out), "out=%q", out)
		requireTruncationOf(t, string(out), record)
	})
}

// requireTruncationOf asserts that repaired is a faithful truncation of
// original: containers hold a subset of the original members, and every
// scalar equals the original or is a marker-terminated prefix of a string.
func requireTruncationOf(t *testing.T, repaired, original string) {
	t.Helper()
	var rep, orig any
	require.NoError(t, decodeJSONNumber(repaired, &rep))
	require.NoError(t, decodeJSONNumber(original, &orig))
	require.True(t, isTruncationOf(rep, orig), "repaired %q is not a truncation of %q", repaired, original)
}

func isTruncationOf(rep, orig any) bool {
	switch r := rep.(type) {
	case map[string]any:
		o, ok := orig.(map[string]any)
		if !ok {
			return false
		}
		for k, rv := range r {
			ov, ok := o[k]
			if !ok || !isTruncationOf(rv, ov) {
				return false
			}
		}
		return true
	case []any:
		o, ok := orig.([]any)
		if !ok || len(r) > len(o) {
			return false
		}
		for i := range r {
			if !isTruncationOf(r[i], o[i]) {
				return false
			}
		}
		return true
	case string:
		o, ok := orig.(string)
		if !ok {
			return false
		}
		if r == o {
			return true
		}
		prefix, cut := strings.CutSuffix(r, truncatedJSONMarker)
		return cut && strings.HasPrefix(o, prefix)
	default:
		return rep == orig
	}
}

func decodeJSONNumber(s string, v *any) error {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	return dec.Decode(v)
}

func hasDuplicateKeys(s string) bool {
	dec := json.NewDecoder(bytes.NewReader([]byte(s)))
	type frame struct {
		object    bool
		keys      map[string]bool
		expectKey bool
	}
	var stack []*frame
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return false
		}
		if err != nil {
			return true
		}
		var top *frame
		if len(stack) > 0 {
			top = stack[len(stack)-1]
		}
		switch v := tok.(type) {
		case json.Delim:
			switch v {
			case '{', '[':
				if top != nil && top.object {
					top.expectKey = true
				}
				stack = append(stack, &frame{object: v == '{', keys: map[string]bool{}, expectKey: true})
			default:
				stack = stack[:len(stack)-1]
			}
			continue
		case string:
			if top != nil && top.object && top.expectKey {
				if top.keys[v] {
					return true
				}
				top.keys[v] = true
				top.expectKey = false
				continue
			}
		}
		if top != nil && top.object {
			top.expectKey = true
		}
	}
}

func startTruncateJSONInput(t *testing.T) (net.Conn, chan *entry.Entry) {
	t.Helper()
	cfg := NewConfigWithID("test_id")
	cfg.ListenAddress = "127.0.0.1:0"
	cfg.MaxLogSize = minMaxLogSize
	cfg.TruncateJSON = true
	op, err := cfg.Build(componenttest.NewNopTelemetrySettings())
	require.NoError(t, err)

	tcpInput := op.(*Input)
	mockOutput := testutil.Operator{}
	tcpInput.OutputOperators = []operator.Operator{&mockOutput}
	entryChan := make(chan *entry.Entry, 8)
	mockOutput.On("Process", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		entryChan <- args.Get(1).(*entry.Entry)
	}).Return(nil)

	require.NoError(t, tcpInput.Start(testutil.NewUnscopedMockPersister()))
	t.Cleanup(func() { require.NoError(t, tcpInput.Stop()) })
	conn, err := net.Dial("tcp", tcpInput.listener.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	return conn, entryChan
}

func nextEntry(t *testing.T, entryChan chan *entry.Entry) *entry.Entry {
	t.Helper()
	select {
	case e := <-entryChan:
		return e
	case <-time.After(2 * time.Second):
		require.FailNow(t, "timed out waiting for entry")
		return nil
	}
}

func TestTCPInputTruncateJSON(t *testing.T) {
	conn, entryChan := startTruncateJSONInput(t)

	oversized := `{"tag":"cockroach.sql_exec","channel":"SQL_EXEC","event":{"EventType":"sampled_query","Statement":"INSERT INTO t VALUES ` +
		strings.Repeat(`(1, \"a\"), `, minMaxLogSize) + `","User":"root"}}`
	_, err := conn.Write([]byte("{\"before\":1}\n" + oversized + "\n{\"after\":2}\n"))
	require.NoError(t, err)

	require.Equal(t, `{"before":1}`, nextEntry(t, entryChan).Body)

	e := nextEntry(t, entryChan)
	require.Equal(t, true, e.Attributes[TruncatedAttribute])
	body, ok := e.Body.(string)
	require.True(t, ok)
	require.LessOrEqual(t, len(body), minMaxLogSize)
	require.True(t, json.Valid([]byte(body)), "body should be valid JSON: %.200q", body)
	var parsed struct {
		Tag   string `json:"tag"`
		Event struct {
			EventType string `json:"EventType"`
			Statement string `json:"Statement"`
		} `json:"event"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &parsed))
	require.Equal(t, "cockroach.sql_exec", parsed.Tag)
	require.Equal(t, "sampled_query", parsed.Event.EventType)
	require.True(t, strings.HasPrefix(parsed.Event.Statement, `INSERT INTO t VALUES (1, "a"), `))
	require.True(t, strings.HasSuffix(parsed.Event.Statement, truncatedJSONMarker))

	require.Equal(t, `{"after":2}`, nextEntry(t, entryChan).Body)
}

func TestTCPInputTruncateJSONKeepsNonJSONPrefix(t *testing.T) {
	conn, entryChan := startTruncateJSONInput(t)

	oversized := strings.Repeat("goroutine 1 [running]\t", minMaxLogSize)
	_, err := conn.Write([]byte(oversized + "\nnext\n"))
	require.NoError(t, err)

	e := nextEntry(t, entryChan)
	require.Equal(t, true, e.Attributes[TruncatedAttribute])
	require.Equal(t, oversized[:minMaxLogSize], e.Body)
	require.Equal(t, "next", nextEntry(t, entryChan).Body)
}

func TestTruncateJSONConfigValidation(t *testing.T) {
	build := func(mutate func(*Config)) error {
		cfg := NewConfigWithID("test_id")
		cfg.ListenAddress = "127.0.0.1:0"
		cfg.TruncateJSON = true
		mutate(cfg)
		_, err := cfg.Build(componenttest.NewNopTelemetrySettings())
		return err
	}
	require.NoError(t, build(func(*Config) {}))
	require.NoError(t, build(func(c *Config) { c.Encoding = "utf-8-raw" }))
	require.ErrorContains(t, build(func(c *Config) { c.Encoding = "utf-16" }), "requires utf-8")
	require.ErrorContains(t, build(func(c *Config) { c.Encoding = "nop" }), "requires utf-8")
	require.ErrorContains(t, build(func(c *Config) { c.SplitConfig.LineStartPattern = "^{" }), "multiline")
}
