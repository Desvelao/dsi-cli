package pyutil

import (
	"bytes"
	"encoding/json"
)

// JSONDumps encodes v like Python's json.dumps(v, ensure_ascii=False): the
// ", " and ": " separators, no HTML escaping and U+2028/U+2029 left as-is.
func JSONDumps(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	compact := bytes.TrimRight(buf.Bytes(), "\n")

	var out bytes.Buffer
	inString := false
	for i := 0; i < len(compact); i++ {
		c := compact[i]
		if inString {
			switch {
			case c == '\\' && i+5 < len(compact) && compact[i+1] == 'u' &&
				(string(compact[i+2:i+6]) == "2028" || string(compact[i+2:i+6]) == "2029"):
				if compact[i+5] == '8' {
					out.WriteString(" ")
				} else {
					out.WriteString(" ")
				}
				i += 5
			case c == '\\':
				out.WriteByte(c)
				i++
				out.WriteByte(compact[i])
			case c == '"':
				inString = false
				out.WriteByte(c)
			default:
				out.WriteByte(c)
			}
			continue
		}
		switch c {
		case '"':
			inString = true
			out.WriteByte(c)
		case ',', ':':
			out.WriteByte(c)
			out.WriteByte(' ')
		default:
			out.WriteByte(c)
		}
	}
	return out.String(), nil
}
