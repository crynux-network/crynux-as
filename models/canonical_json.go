package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// CanonicalJSON returns deterministic JSON for maps with sorted keys.
// A nil value yields the bytes for JSON null.
func CanonicalJSON(v any) ([]byte, error) {
	if v == nil {
		return []byte("null"), nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var generic any
	if err := decoder.Decode(&generic); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	writeCanonicalValue(&buf, generic)
	return buf.Bytes(), nil
}

// QuantizationConfigKey returns the canonical JSON string of a quantization_config
// value for use as a cache key. A nil or JSON-null config yields "null".
func QuantizationConfigKey(config any) (string, error) {
	if config == nil {
		return "null", nil
	}
	if raw, ok := config.(json.RawMessage); ok {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
			return "null", nil
		}
		var parsed any
		decoder := json.NewDecoder(bytes.NewReader(trimmed))
		decoder.UseNumber()
		if err := decoder.Decode(&parsed); err != nil {
			return "", err
		}
		canonical, err := CanonicalJSON(parsed)
		if err != nil {
			return "", err
		}
		return string(canonical), nil
	}
	canonical, err := CanonicalJSON(config)
	if err != nil {
		return "", err
	}
	return string(canonical), nil
}

func writeCanonicalValue(buf *bytes.Buffer, v any) {
	switch value := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		buf.WriteString(strconv.FormatBool(value))
	case json.Number:
		buf.WriteString(value.String())
	case string:
		writeCanonicalString(buf, value)
	case []any:
		buf.WriteByte('[')
		for i, item := range value {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeCanonicalValue(buf, item)
		}
		buf.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		buf.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeCanonicalString(buf, key)
			buf.WriteByte(':')
			writeCanonicalValue(buf, value[key])
		}
		buf.WriteByte('}')
	}
}

func writeCanonicalString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		default:
			if r < 0x20 {
				fmt.Fprintf(buf, `\u%04x`, r)
			} else {
				buf.WriteRune(r)
			}
		}
	}
	buf.WriteByte('"')
}
