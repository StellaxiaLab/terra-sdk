package trust

import "testing"

// Expected values were produced by the pre-extraction implementation (the Terra
// node's module runtime) and pin the canonical payload bytes: key order, HTML
// escaping, float64 numbers, invalid UTF-8 replacement, and which part of the
// `signature` block each payload keeps.
func TestCanonicalPayloadGolden(t *testing.T) {
	cases := []struct{ in, author, counter string }{
		{`{"id":"a.b.c","version":"1.0.0","x":"<tag>&amp; \u2028","n":1e21,"f":0.1,"big":12345678901234567890,"signature":{"algorithm":"ed25519","chain":[1]},"z":[{"b":1,"a":2}]}`,
			`{"big":12345678901234567000,"f":0.1,"id":"a.b.c","n":1e+21,"version":"1.0.0","x":"\u003ctag\u003e\u0026amp; \u2028","z":[{"a":2,"b":1}]}`,
			`{"big":12345678901234567000,"f":0.1,"id":"a.b.c","n":1e+21,"signature":{"algorithm":"ed25519"},"version":"1.0.0","x":"\u003ctag\u003e\u0026amp; \u2028","z":[{"a":2,"b":1}]}`},
		{`{"z":1,"a":{"k":"\ud800","j":null},"signature":{"value":"x"}}`,
			"{\"a\":{\"j\":null,\"k\":\"\ufffd\"},\"z\":1}",
			"{\"a\":{\"j\":null,\"k\":\"\ufffd\"},\"signature\":{\"value\":\"x\"},\"z\":1}"},
	}
	for _, c := range cases {
		author, err := CanonicalManifestPayload([]byte(c.in))
		if err != nil || string(author) != c.author {
			t.Errorf("author payload of %s\n got %s (%v)\nwant %s", c.in, author, err, c.author)
		}
		counter, err := CanonicalCounterSignPayload([]byte(c.in))
		if err != nil || string(counter) != c.counter {
			t.Errorf("counter payload of %s\n got %s (%v)\nwant %s", c.in, counter, err, c.counter)
		}
	}
	if _, err := CanonicalManifestPayload([]byte("not json")); err == nil {
		t.Error("malformed JSON must be refused")
	}
}
