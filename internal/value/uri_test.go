package value

import "testing"

// The uri domain is RFC 3986's URI production: what its ABNF derives, and nothing a URI library
// adds or drops.
func TestURIIsTheRFC3986Production(t *testing.T) {
	for _, s := range []string{
		"https://example.com/a?b=c", "http://my_host.com", "mailto:ken@example.com", "urn:isbn:0451450523",
		"file:///etc/hosts", "a:b", "a:?q", "a:/", "a:///", "a+b-c.d:e", "HTTP://EXAMPLE.COM",
		// An empty path, an empty authority and an empty host are in the grammar.
		"a:", "a:#f", "a://", "http:", "http://", "https://", "http://host?", "http://host#",
		// A port is any number of digits.
		"http://[::1]:2147483648/", "http://example.com:99999999999/", "http://host:/",
		// IP literals: IPv6 in its forms, IPv4 embedded, and IPvFuture.
		"http://[::1]/", "http://[::]/", "http://[1:2:3:4:5:6:7:8]/", "http://[1::8]/",
		"http://[::ffff:192.0.2.1]/", "http://[1:2:3:4:5:6:1.2.3.4]/", "http://[v1.abc]/", "http://[vF.a:b]/", "http://[V1.abc]/",
		"http://192.0.2.1/", "http://user:pw@host/", "http://us%40er@host/", "http://%41.example/",
		"http://[::1]:80/%7e?q=%20#%41", "data:text/plain;base64,SGVsbG8=", "a:b/c//d",
	} {
		if !IsURI(s) {
			t.Errorf("%q is a URI", s)
		}
	}
	for _, s := range []string{
		"", "foo/bar", "#top", "//host/path", "/path", "1a:b", "://x", "a b:c",
		"http://host/%4", "http://host/%zz", "http://%zz.example/", "http://host/ a", "http://host/#a#b",
		"http://日本.jp/", "http://host/日本", "http://[::1", "http://::1/",
		// RFC 9844 removed the zone identifier RFC 6874 had added.
		"http://[fe80::1%25eth0]/", "http://[fe80::1%eth0]/",
		"http://[2001:db8::g]/", "http://[1:2:3:4:5:6:7:8:9]/", "http://[1::2::3]/", "http://[::256.0.0.1]/",
		"http://[v.abc]/", "http://[v1.]/", "http://host:8x/", "http://ho[st/", "a:b|c",
	} {
		if IsURI(s) {
			t.Errorf("%q is not a URI", s)
		}
	}
}
