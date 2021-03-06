package dns

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Parse the rdata of each rrtype.
// All data from the channel c is either _STRING or _BLANK.
// After the rdata there may come 1 _BLANK and then a _NEWLINE
// or immediately a _NEWLINE. If this is not the case we flag
// an *ParseError: garbage after rdata.

func setRR(h RR_Header, c chan lex, o, f string) (RR, *ParseError) {
	var r RR
	e := new(ParseError)
	switch h.Rrtype {
	case TypeA:
		r, e = setA(h, c, f)
		goto Slurp
	case TypeAAAA:
		r, e = setAAAA(h, c, f)
		goto Slurp
	case TypeNS:
		r, e = setNS(h, c, o, f)
		goto Slurp
	case TypeMX:
		r, e = setMX(h, c, o, f)
		goto Slurp
	case TypeCNAME:
		r, e = setCNAME(h, c, o, f)
		goto Slurp
	case TypeSOA:
		r, e = setSOA(h, c, o, f)
		goto Slurp
	case TypeSSHFP:
		r, e = setSSHFP(h, c, f)
		goto Slurp
	case TypeDNSKEY:
		// These types have a variable ending either chunks of txt or chunks/base64 or hex.
		// They need to search for the end of the RR themselves, hence they look for the ending
		// newline. Thus there is no need to slurp the remainder, because there is none.
		return setDNSKEY(h, c, f)
	case TypeRRSIG:
		return setRRSIG(h, c, o, f)
	case TypeNSEC:
		return setNSEC(h, c, o, f)
	case TypeNSEC3:
		return setNSEC3(h, c, o, f)
	case TypeDS:
		return setDS(h, c, f)
	case TypeTXT:
		return setTXT(h, c, f)
	default:
		// Don't the have the token the holds the RRtype, but we substitute that in the
		// calling function when lex is empty.
		return nil, &ParseError{f, "Unknown RR type", lex{}}
	}
Slurp:
	if e != nil {
		return nil, e
	}
	if se := slurpRemainder(c, f); se != nil {
		return nil, se
	}
	return r, e
}

func slurpRemainder(c chan lex, f string) *ParseError {
	l := <-c
	if _DEBUG {
		fmt.Printf("%v\n", l)
	}
	switch l.value {
	case _BLANK:
		l = <-c
		if _DEBUG {
			fmt.Printf("%v\n", l)
		}
		if l.value != _NEWLINE && l.value != _EOF {
			return &ParseError{f, "garbage after rdata", l}
		}
		// Ok
	case _NEWLINE:
		// Ok
	case _EOF:
		// Ok
	default:
		return &ParseError{f, "garbage after directly rdata", l}
	}
	return nil
}

func setA(h RR_Header, c chan lex, f string) (RR, *ParseError) {
	rr := new(RR_A)
	rr.Hdr = h

	l := <-c
	rr.A = net.ParseIP(l.token)
	if rr.A == nil {
		return nil, &ParseError{f, "bad A", l}
	}
	return rr, nil
}

func setAAAA(h RR_Header, c chan lex, f string) (RR, *ParseError) {
	rr := new(RR_AAAA)
	rr.Hdr = h

	l := <-c
	rr.AAAA = net.ParseIP(l.token)
	if rr.AAAA == nil {
		return nil, &ParseError{f, "bad AAAA", l}
	}
	return rr, nil
}

func setNS(h RR_Header, c chan lex, o, f string) (RR, *ParseError) {
	rr := new(RR_NS)
	rr.Hdr = h

	l := <-c
	rr.Ns = l.token
	if _, ok := IsDomainName(l.token); !ok {
		return nil, &ParseError{f, "bad NS Ns", l}
	}
	if !IsFqdn(rr.Ns) {
		rr.Ns += o
	}
	return rr, nil
}

func setMX(h RR_Header, c chan lex, o, f string) (RR, *ParseError) {
	rr := new(RR_MX)
	rr.Hdr = h

	l := <-c
	if i, e := strconv.Atoi(l.token); e != nil {
		return nil, &ParseError{f, "bad MX Pref", l}
	} else {
		rr.Pref = uint16(i)
	}
	<-c     // _BLANK
	l = <-c // _STRING
	rr.Mx = l.token
	if _, ok := IsDomainName(l.token); !ok {
		return nil, &ParseError{f, "bad MX Mx", l}
	}
	if !IsFqdn(rr.Mx) {
		rr.Mx += o
	}
	return rr, nil
}

func setCNAME(h RR_Header, c chan lex, o, f string) (RR, *ParseError) {
	rr := new(RR_CNAME)
	rr.Hdr = h

	l := <-c
	rr.Cname = l.token
	if _, ok := IsDomainName(l.token); !ok {
		return nil, &ParseError{f, "bad CNAME", l}
	}
	if !IsFqdn(rr.Cname) {
		rr.Cname += o
	}
	return rr, nil
}

func setSOA(h RR_Header, c chan lex, o, f string) (RR, *ParseError) {
	rr := new(RR_SOA)
	rr.Hdr = h

	l := <-c
	rr.Ns = l.token
	<-c // _BLANK
	if _, ok := IsDomainName(l.token); !ok {
		return nil, &ParseError{f, "bad SOA mname", l}
	}
	if !IsFqdn(rr.Ns) {
		rr.Ns += o
	}

	l = <-c
	rr.Mbox = l.token
	if _, ok := IsDomainName(l.token); !ok {
		return nil, &ParseError{f, "bad SOA rname", l}
	}
	if !IsFqdn(rr.Mbox) {
		rr.Mbox += o
	}
	<-c // _BLANK

	var j int
	var e error
	for i := 0; i < 5; i++ {
		l = <-c
		if j, e = strconv.Atoi(l.token); e != nil {
			return nil, &ParseError{f, "bad SOA zone parameter", l}
		}
		switch i {
		case 0:
			rr.Serial = uint32(j)
			<-c // _BLANK
		case 1:
			rr.Refresh = uint32(j)
			<-c // _BLANK
		case 2:
			rr.Retry = uint32(j)
			<-c // _BLANK
		case 3:
			rr.Expire = uint32(j)
			<-c // _BLANK
		case 4:
			rr.Minttl = uint32(j)
		}
	}
	return rr, nil
}

func setRRSIG(h RR_Header, c chan lex, o, f string) (RR, *ParseError) {
	rr := new(RR_RRSIG)
	rr.Hdr = h
	l := <-c
	if t, ok := Str_rr[strings.ToUpper(l.token)]; !ok {
		return nil, &ParseError{f, "bad RRSIG", l}
	} else {
		rr.TypeCovered = t
	}
	<-c // _BLANK
	l = <-c
	if i, err := strconv.Atoi(l.token); err != nil {
		return nil, &ParseError{f, "bad RRSIG", l}
	} else {
		rr.Algorithm = uint8(i)
	}
	<-c // _BLANK
	l = <-c
	if i, err := strconv.Atoi(l.token); err != nil {
		return nil, &ParseError{f, "bad RRSIG", l}
	} else {
		rr.Labels = uint8(i)
	}
	<-c // _BLANK
	l = <-c
	if i, err := strconv.Atoi(l.token); err != nil {
		return nil, &ParseError{f, "bad RRSIG", l}
	} else {
		rr.OrigTtl = uint32(i)
	}
	<-c // _BLANK
	l = <-c
	if i, err := dateToTime(l.token); err != nil {
		return nil, &ParseError{f, "bad RRSIG expiration", l}
	} else {
		rr.Expiration = i
	}
	<-c // _BLANK
	l = <-c
	if i, err := dateToTime(l.token); err != nil {
		return nil, &ParseError{f, "bad RRSIG inception", l}
	} else {
		rr.Inception = i
	}
	<-c // _BLANK
	l = <-c
	if i, err := strconv.Atoi(l.token); err != nil {
		return nil, &ParseError{f, "bad RRSIG keytag", l}
	} else {
		rr.KeyTag = uint16(i)
	}
	<-c // _BLANK
	l = <-c
	rr.SignerName = l.token
	if _, ok := IsDomainName(l.token); !ok {
		return nil, &ParseError{f, "bad RRSIG signername", l}
	}
	if !IsFqdn(rr.SignerName) {
		rr.SignerName += o
	}
	// Get the remaining data until we see a NEWLINE
	l = <-c
	s := ""
	for l.value != _NEWLINE && l.value != _EOF {
		switch l.value {
		case _STRING:
			s += l.token
		case _BLANK:
			// Ok
		default:
			return nil, &ParseError{f, "bad RRSIG signature", l}
		}
		l = <-c
	}
	rr.Signature = s
	return rr, nil
}

func setNSEC(h RR_Header, c chan lex, o, f string) (RR, *ParseError) {
	rr := new(RR_NSEC)
	rr.Hdr = h

	l := <-c
	rr.NextDomain = l.token
	if _, ok := IsDomainName(l.token); !ok {
		return nil, &ParseError{f, "bad NSEC nextdomain", l}
	}
	if !IsFqdn(rr.NextDomain) {
		rr.NextDomain += o
	}

	rr.TypeBitMap = make([]uint16, 0)
	l = <-c
	for l.value != _NEWLINE && l.value != _EOF {
		switch l.value {
		case _BLANK:
			// Ok
		case _STRING:
			if k, ok := Str_rr[strings.ToUpper(l.token)]; !ok {
				return nil, &ParseError{f, "bad NSEC non RR in type bitmap", l}
			} else {
				rr.TypeBitMap = append(rr.TypeBitMap, k)
			}
		default:
			return nil, &ParseError{f, "bad NSEC garbage in type bitmap", l}
		}
		l = <-c
	}
	return rr, nil
}

func setNSEC3(h RR_Header, c chan lex, o, f string) (RR, *ParseError) {
	rr := new(RR_NSEC3)
	rr.Hdr = h

	l := <-c
	if i, e := strconv.Atoi(l.token); e != nil {
		return nil, &ParseError{f, "bad NSEC3", l}
	} else {
		rr.Hash = uint8(i)
	}
	<-c // _BLANK
	l = <-c
	if i, e := strconv.Atoi(l.token); e != nil {
		return nil, &ParseError{f, "bad NSEC3", l}
	} else {
		rr.Flags = uint8(i)
	}
	<-c // _BLANK
	l = <-c
	if i, e := strconv.Atoi(l.token); e != nil {
		return nil, &ParseError{f, "bad NSEC3", l}
	} else {
		rr.Iterations = uint16(i)
	}
	<-c
	l = <-c
	rr.SaltLength = uint8(len(l.token))
	rr.Salt = l.token // CHECK?

	<-c
	l = <-c
	rr.HashLength = uint8(len(l.token))
	rr.NextDomain = l.token
	if _, ok := IsDomainName(l.token); !ok {
		return nil, &ParseError{f, "bad NSEC nextdomain", l}
	}
	if !IsFqdn(rr.NextDomain) {
		rr.NextDomain += o
	}

	rr.TypeBitMap = make([]uint16, 0)
	l = <-c
	for l.value != _NEWLINE && l.value != _EOF {
		switch l.value {
		case _BLANK:
			// Ok
		case _STRING:
			if k, ok := Str_rr[strings.ToUpper(l.token)]; !ok {
				return nil, &ParseError{f, "bad NSEC3", l}
			} else {
				rr.TypeBitMap = append(rr.TypeBitMap, k)
			}
		default:
			return nil, &ParseError{f, "bad NSEC3", l}
		}
		l = <-c
	}
	return rr, nil
}

/*
func setNSEC3PARAM(h RR_Header, c chan lex) (RR, *ParseError) {
        rr := new(RR_NSEC3PARAM)
        rr.Hdr = h
        l := <-c
        if i, e = strconv.Atoi(rdf[0]); e != nil {
                return nil, &ParseError{Error: "bad NSEC3PARAM", name: rdf[0], line: l}
        } else {
        rr.Hash = uint8(i)
}
        if i, e = strconv.Atoi(rdf[1]); e != nil {
                reutrn nil, &ParseError{Error: "bad NSEC3PARAM", name: rdf[1], line: l}
        } else {
        rr.Flags = uint8(i)
}
        if i, e = strconv.Atoi(rdf[2]); e != nil {
                return nil, &ParseError{Error: "bad NSEC3PARAM", name: rdf[2], line: l}
        } else {
        rr.Iterations = uint16(i)
}
        rr.Salt = rdf[3]
        rr.SaltLength = uint8(len(rr.Salt))
        zp.RR <- rr
    }
*/

func setSSHFP(h RR_Header, c chan lex, f string) (RR, *ParseError) {
	rr := new(RR_SSHFP)
	rr.Hdr = h

	l := <-c
	if i, e := strconv.Atoi(l.token); e != nil {
		return nil, &ParseError{f, "bad SSHFP", l}
	} else {
		rr.Algorithm = uint8(i)
	}
	<-c // _BLANK
	l = <-c
	if i, e := strconv.Atoi(l.token); e != nil {
		return nil, &ParseError{f, "bad SSHFP", l}
	} else {
		rr.Type = uint8(i)
	}
	<-c // _BLANK
	l = <-c
	rr.FingerPrint = l.token
	return rr, nil
}

func setDNSKEY(h RR_Header, c chan lex, f string) (RR, *ParseError) {
	rr := new(RR_DNSKEY)
	rr.Hdr = h

	l := <-c
	if i, e := strconv.Atoi(l.token); e != nil {
		return nil, &ParseError{f, "bad DNSKEY", l}
	} else {
		rr.Flags = uint16(i)
	}
	<-c     // _BLANK
	l = <-c // _STRING
	if i, e := strconv.Atoi(l.token); e != nil {
		return nil, &ParseError{f, "bad DNSKEY", l}
	} else {
		rr.Protocol = uint8(i)
	}
	<-c     // _BLANK
	l = <-c // _STRING
	if i, e := strconv.Atoi(l.token); e != nil {
		return nil, &ParseError{f, "bad DNSKEY", l}
	} else {
		rr.Algorithm = uint8(i)
	}
	l = <-c
	var s string
	for l.value != _NEWLINE && l.value != _EOF {
		switch l.value {
		case _STRING:
			s += l.token
		case _BLANK:
			// Ok
		default:
			return nil, &ParseError{f, "bad DNSKEY", l}
		}
		l = <-c
	}
	rr.PublicKey = s
	return rr, nil
}

// DLV and TA are the same
func setDS(h RR_Header, c chan lex, f string) (RR, *ParseError) {
	rr := new(RR_DS)
	rr.Hdr = h
	l := <-c
	if i, e := strconv.Atoi(l.token); e != nil {
		return nil, &ParseError{f, "bad DS", l}
	} else {
		rr.KeyTag = uint16(i)
	}
	<-c // _BLANK
	l = <-c
	if i, e := strconv.Atoi(l.token); e != nil {
		return nil, &ParseError{f, "bad DS", l}
	} else {
		rr.Algorithm = uint8(i)
	}
	<-c // _BLANK
	l = <-c
	if i, e := strconv.Atoi(l.token); e != nil {
		return nil, &ParseError{f, "bad DS", l}
	} else {
		rr.DigestType = uint8(i)
	}
	// There can be spaces here...
	l = <-c
	s := ""
	for l.value != _NEWLINE && l.value != _EOF {
		switch l.value {
		case _STRING:
			s += l.token
		case _BLANK:
			// Ok
		default:
			return nil, &ParseError{f, "bad DS", l}
		}
		l = <-c
	}
	rr.Digest = s
	return rr, nil
}

func setTXT(h RR_Header, c chan lex, f string) (RR, *ParseError) {
	rr := new(RR_TXT)
	rr.Hdr = h

	// Get the remaining data until we see a NEWLINE
	l := <-c
	var s string
	for l.value != _NEWLINE && l.value != _EOF {
		switch l.value {
		case _STRING:
			s += l.token
		case _BLANK:
			s += l.token
		default:
			return nil, &ParseError{f, "bad TXT", l}
		}
		l = <-c
	}
	rr.Txt = s
	return rr, nil
}
