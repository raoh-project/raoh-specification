package value

import (
	"regexp"
	"strconv"
	"strings"
)

// uriPattern is the URI production of RFC 3986 section 3, written from its ABNF (appendix A), with
// nothing a URI library adds or leaves out: a scheme is required, a host may be empty, a port may
// have any number of digits, IPvFuture is a host, and an IPv6 address has no zone identifier
// (RFC 6874 added one and RFC 9844 removed it again). Every rule is matched in full, so the
// pattern accepts exactly the strings the grammar derives. A quoted string in ABNF matches either
// case (RFC 5234 section 2.3); of RFC 3986's, only IPvFuture's "v" has a letter, so it is [vV].
var uriPattern = regexp.MustCompile(`^` + uriRule() + `$`)

// IsURI reports whether s is a URI as RFC 3986 section 3 defines it.
func IsURI(s string) bool { return uriPattern.MatchString(s) }

func uriRule() string {
	const (
		alpha      = `A-Za-z`
		digit      = `0-9`
		hexdig     = `0-9A-Fa-f`
		unreserved = alpha + digit + `\-._~`
		subDelims  = `!$&'()*+,;=`
	)
	pct := `%[` + hexdig + `]{2}`
	group := func(chars string) string { return `(?:[` + chars + `]|` + pct + `)` }

	h16 := `[` + hexdig + `]{1,4}`
	decOctet := `(?:25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9][0-9]|[0-9])`
	ipv4 := decOctet + `(?:\.` + decOctet + `){3}`
	ls32 := `(?:` + h16 + `:` + h16 + `|` + ipv4 + `)`
	rep := func(n int) string { // n times h16 ":"
		return strings.Repeat(h16+`:`, n)
	}
	upTo := func(n int) string { // [ *n( h16 ":" ) h16 ]
		if n == 0 {
			return `(?:` + h16 + `)?`
		}
		return `(?:(?:` + h16 + `:){0,` + strconv.Itoa(n) + `}` + h16 + `)?`
	}
	ipv6 := `(?:` + strings.Join([]string{
		rep(6) + ls32,
		`::` + rep(5) + ls32,
		upTo(0) + `::` + rep(4) + ls32,
		upTo(1) + `::` + rep(3) + ls32,
		upTo(2) + `::` + rep(2) + ls32,
		upTo(3) + `::` + rep(1) + ls32,
		upTo(4) + `::` + ls32,
		upTo(5) + `::` + h16,
		upTo(6) + `::`,
	}, `|`) + `)`
	ipvFuture := `[vV][` + hexdig + `]+\.[` + unreserved + subDelims + `:]+`
	ipLiteral := `\[(?:` + ipv6 + `|` + ipvFuture + `)\]`
	regName := group(unreserved+subDelims) + `*`
	host := `(?:` + ipLiteral + `|` + ipv4 + `|` + regName + `)`
	userinfo := group(unreserved+subDelims+`:`) + `*`
	authority := `(?:` + userinfo + `@)?` + host + `(?::[` + digit + `]*)?`

	pchar := group(unreserved + subDelims + `:@`)
	segment := pchar + `*`
	segmentNZ := pchar + `+`
	pathAbempty := `(?:/` + segment + `)*`
	pathAbsolute := `/(?:` + segmentNZ + `(?:/` + segment + `)*)?`
	pathRootless := segmentNZ + `(?:/` + segment + `)*`
	hierPart := `(?://` + authority + pathAbempty + `|` + pathAbsolute + `|` + pathRootless + `|)`

	scheme := `[` + alpha + `][` + alpha + digit + `+\-.]*`
	// query and fragment are the same production: *( pchar / "/" / "?" ).
	queryOrFragment := group(unreserved+subDelims+`:@/?`) + `*`
	return scheme + `:` + hierPart + `(?:\?` + queryOrFragment + `)?(?:#` + queryOrFragment + `)?`
}
