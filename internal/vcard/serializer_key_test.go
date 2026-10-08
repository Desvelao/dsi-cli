package vcard

import "testing"

func TestBuildContentKeyRejectsInjection(t *testing.T) {
	cases := map[string]KeySpec{
		"alg newline":   {Alg: "ed25519\nX-ENDORSE:evil", KeyB64: "AAAA"},
		"key newline":   {Alg: "ed25519", KeyB64: "AAAA\r\nREVKEY:x"},
		"enc newline":   {Alg: "ed25519", KeyB64: "AAAA", Encoding: "b64\nX-A:1"},
		"alg separator": {Alg: "ed;25519", KeyB64: "AAAA"},
		"alg colon":     {Alg: "ed:25519", KeyB64: "AAAA"},
		"enc quote":     {Alg: "ed25519", KeyB64: "AAAA", Encoding: `b"64`},
		"enc semicolon": {Alg: "ed25519", KeyB64: "AAAA", Encoding: "a;B=c"},
	}
	for name, k := range cases {
		if _, err := BuildContent(Fields{Keys: []KeySpec{k}}); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestBuildContentKeyValid(t *testing.T) {
	if _, err := BuildContent(Fields{Keys: []KeySpec{{Alg: "Ed25519", KeyB64: "AAAA"}}}); err != nil {
		t.Fatal(err)
	}
}
