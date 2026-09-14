package volumes

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
)

// diskutil's -plist output is XML. Decode the small subset of plist types it
// uses without spawning a converter or depending on Python on the user's Mac.
func parsePlist(data []byte) (map[string]any, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	var read func(xml.StartElement) (any, error)
	read = func(start xml.StartElement) (any, error) {
		switch start.Name.Local {
		case "dict":
			out := map[string]any{}
			key := ""
			for {
				token, err := d.Token()
				if err != nil {
					return nil, err
				}
				switch t := token.(type) {
				case xml.EndElement:
					return out, nil
				case xml.StartElement:
					if t.Name.Local == "key" {
						if err := d.DecodeElement(&key, &t); err != nil {
							return nil, err
						}
					} else {
						value, err := read(t)
						if err != nil {
							return nil, err
						}
						out[key] = value
					}
				}
			}
		case "array":
			var out []any
			for {
				token, err := d.Token()
				if err != nil {
					return nil, err
				}
				switch t := token.(type) {
				case xml.EndElement:
					return out, nil
				case xml.StartElement:
					value, err := read(t)
					if err != nil {
						return nil, err
					}
					out = append(out, value)
				}
			}
		default:
			var value string
			if err := d.DecodeElement(&value, &start); err != nil {
				return nil, err
			}
			switch start.Name.Local {
			case "integer":
				return strconv.ParseUint(value, 10, 64)
			case "true":
				return true, nil
			case "false":
				return false, nil
			default:
				return value, nil
			}
		}
	}
	for {
		token, err := d.Token()
		if err != nil {
			if err == io.EOF {
				return nil, fmt.Errorf("plist has no dictionary")
			}
			return nil, err
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "dict" {
			value, err := read(start)
			if err != nil {
				return nil, err
			}
			return value.(map[string]any), nil
		}
	}
}

func stringValue(m map[string]any, key string) string { s, _ := m[key].(string); return s }
func uintValue(m map[string]any, key string) uint64   { n, _ := m[key].(uint64); return n }
func boolValue(m map[string]any, key string) bool     { b, _ := m[key].(bool); return b }
