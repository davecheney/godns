package main

import (
	"dns"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

var dnskey *dns.RR_DNSKEY

func q(w dns.RequestWriter, m *dns.Msg) {
	w.Send(m)
	r, err := w.Receive()
	if err != nil {
		fmt.Printf("%s\n", err.Error())
	}
	w.Write(r)
}

func main() {
	dnssec := flag.Bool("dnssec", false, "request DNSSEC records")
	query := flag.Bool("question", false, "show question")
	short := flag.Bool("short", false, "abbreviate long DNSSEC records")
	check := flag.Bool("check", false, "check internal DNSSEC consistency")
        anchor := flag.String("anchor", "", "use the DNSKEY in this file for checking consistency")
	port := flag.Int("port", 53, "port number to use")
	aa := flag.Bool("aa", false, "set AA flag in query")
	ad := flag.Bool("ad", false, "set AD flag in query")
	cd := flag.Bool("cd", false, "set CD flag in query")
	rd := flag.Bool("rd", true, "unset RD flag in query")
	tcp := flag.Bool("tcp", false, "TCP mode")
	nsid := flag.Bool("nsid", false, "ask for NSID")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [@server] [qtype] [qclass] [name ...]\n", os.Args[0])
		flag.PrintDefaults()
	}

	conf, _ := dns.ClientConfigFromFile("/etc/resolv.conf")
	nameserver := "@" + conf.Servers[0]
	qtype := uint16(0)
	qclass := uint16(dns.ClassINET) // Default qclass
	var qname []string

	flag.Parse()
        if *anchor != "" {
                f, err := os.Open(*anchor)
                if err != nil {
                        fmt.Fprintf(os.Stderr, "Failure to open %s: %s\n", *anchor, err.Error())
                }
                r, err := dns.ReadRR(f, *anchor)
                if err != nil {
                        fmt.Fprintf(os.Stderr, "Failure to read an RR from %s: %s\n", *anchor, err.Error())
                }
                if k, ok := r.(*dns.RR_DNSKEY); !ok {
                        fmt.Fprintf(os.Stderr, "No DNSKEY read from %s\n", *anchor)
                } else {
                        dnskey = k
                }
        }

Flags:
	for i := 0; i < flag.NArg(); i++ {
		// If it starts with @ it is a nameserver
		if flag.Arg(i)[0] == '@' {
			nameserver = flag.Arg(i)
			continue Flags
		}
		// First class, then type, to make ANY queries possible
		// And if it looks like type, it is a type
		if k, ok := dns.Str_rr[strings.ToUpper(flag.Arg(i))]; ok {
			qtype = k
			continue Flags
		}
		// If it looks like a class, it is a class
		if k, ok := dns.Str_class[strings.ToUpper(flag.Arg(i))]; ok {
			qclass = k
			continue Flags
		}
		// If it starts with TYPExxx it is unknown rr
		if strings.HasPrefix(flag.Arg(i), "TYPE") {
			i, e := strconv.Atoi(string([]byte(flag.Arg(i))[4:]))
			if e == nil {
				qtype = uint16(i)
				continue Flags
			}
		}

		// Anything else is a qname
		qname = append(qname, flag.Arg(i))
	}
	if len(qname) == 0 {
		qname = make([]string, 1)
		qname[0] = "."
		qtype = dns.TypeNS
	}
	if qtype == 0 {
		qtype = dns.TypeA
	}

	nameserver = string([]byte(nameserver)[1:]) // chop off @
	nameserver += ":" + strconv.Itoa(*port)

	// ipv6 todo
	// We use the async query handling, just to show how
	// it is to be used.
	dns.HandleQueryFunc(".", q)
	dns.ListenAndQuery(nil, nil)
	c := dns.NewClient()
	if *tcp {
		c.Net = "tcp"
	}

	m := new(dns.Msg)
	m.MsgHdr.Authoritative = *aa
	m.MsgHdr.AuthenticatedData = *ad
	m.MsgHdr.CheckingDisabled = *cd
	m.MsgHdr.RecursionDesired = *rd
	m.Question = make([]dns.Question, 1)
	if *dnssec || *nsid {
		o := new(dns.RR_OPT)
		o.Hdr.Name = "."
		o.Hdr.Rrtype = dns.TypeOPT
		if *dnssec {
			o.SetDo()
			o.SetUDPSize(dns.DefaultMsgSize)
		}
		if *nsid {
			o.SetNsid("")
		}
		m.Extra = append(m.Extra, o)
	}

	for _, v := range qname {
		m.Question[0] = dns.Question{v, qtype, qclass}
		m.Id = dns.Id()
		if *query {
			fmt.Printf("%s\n", m.String())
		}
		c.Do(m, nameserver)
	}

	i := 0
forever:
	for {
		select {
		case r := <-dns.DefaultReplyChan:
			if r.Reply != nil {
				if r.Reply.Rcode == dns.RcodeSuccess {
					if r.Request.Id != r.Reply.Id {
						fmt.Printf("Id mismatch\n")
					}
				}
				if *check {
					sigCheck(r.Reply, nameserver, *tcp)
					nsecCheck(r.Reply)
				}
				if *short {
					r.Reply = shortMsg(r.Reply)
				}

				fmt.Printf("%v", r.Reply)
			}
			i++
			if i == len(qname) {
				break forever
			}
		}
	}
}

func sectionCheck(set []dns.RR, server string, tcp bool) {
        var key *dns.RR_DNSKEY
	for _, rr := range set {
		if rr.Header().Rrtype == dns.TypeRRSIG {
			rrset := getRRset(set, rr.Header().Name, rr.(*dns.RR_RRSIG).TypeCovered)
                        if dnskey == nil {
		                key = getKey(rr.(*dns.RR_RRSIG).SignerName, rr.(*dns.RR_RRSIG).KeyTag, server, tcp)
                        } else {
                                key = dnskey
                        }
			if key == nil {
				fmt.Printf(";? DNSKEY %s/%d not found\n", rr.(*dns.RR_RRSIG).SignerName, rr.(*dns.RR_RRSIG).KeyTag)
                                continue
			}
                        where := "net"
                        if dnskey != nil {
                                where = "disk"
                        }
			if err := rr.(*dns.RR_RRSIG).Verify(key, rrset); err != nil {
			        fmt.Printf(";- Bog us signature,  %s does not validate (DNSKEY %s/%d/%s)\n", shortSig(rr.(*dns.RR_RRSIG)), key.Header().Name, key.KeyTag(), where)
			} else {
				fmt.Printf(";+ Secure signature, %s validates (DNSKEY %s/%d/%s)\n", shortSig(rr.(*dns.RR_RRSIG)), key.Header().Name, key.KeyTag(), where)
			}
		}
	}
}

// Check if we have nsec3 records and if so, check them
func nsecCheck(in *dns.Msg) {
        for _, r := range in.Answer {
                if r.Header().Rrtype == dns.TypeNSEC3 {
                        goto Check
                }
        }
        for _, r := range in.Ns {
                if r.Header().Rrtype == dns.TypeNSEC3 {
                        goto Check
                }
        }
        for _, r := range in.Extra {
                if r.Header().Rrtype == dns.TypeNSEC3 {
                        goto Check
                }
        }
        return
Check:
        w, err := in.Nsec3Verify(in.Question[0])
        switch w {
        case dns.NSEC3_NXDOMAIN:
                fmt.Printf(";+ [beta] Correct denial of existence (NSEC3/NXDOMAIN)\n")
        case dns.NSEC3_NODATA:
                fmt.Printf(";+ [beta] Correct denial of existence (NSEC3/NODATA)\n")
        default:
                // w == 0
                if err != nil {
	                fmt.Printf(";- [beta] Incorrect denial of existence (NSEC3): %s\n",err.Error())
                }
        }
}

// Check the sigs in the msg, get the signer's key (additional query), get the 
// rrset from the message, check the signature(s)
func sigCheck(in *dns.Msg, server string, tcp bool) {
        sectionCheck(in.Answer, server, tcp)
        sectionCheck(in.Ns, server, tcp)
        sectionCheck(in.Extra, server, tcp)
}

// Return the RRset belonging to the signature with name and type t
func getRRset(l []dns.RR, name string, t uint16) []dns.RR {
	l1 := make([]dns.RR, 0)
	for _, rr := range l {
		if rr.Header().Name == name && rr.Header().Rrtype == t {
			l1 = append(l1, rr)
		}
	}
	return l1
}

// Get the key from the DNS (uses the local resolver) and return them.
// If nothing is found we return nil
func getKey(name string, keytag uint16, server string, tcp bool) *dns.RR_DNSKEY {
	c := dns.NewClient()
        if tcp {
                c.Net = "tcp"
        }
	m := new(dns.Msg)
	m.SetQuestion(name, dns.TypeDNSKEY)
	r, err := c.Exchange(m, server)
	if err != nil {
		return nil
	}
	for _, k := range r.Answer {
		if k1, ok := k.(*dns.RR_DNSKEY); ok {
			if k1.KeyTag() == keytag {
				return k1
			}
		}
	}
	return nil
}

// shorten RRSIG to "miek.nl RRSIG(NS)"
func shortSig(sig *dns.RR_RRSIG) string {
	return sig.Header().Name + " RRSIG(" + dns.Rr_str[sig.TypeCovered] + ")"
}

// Walk trough message and short Key data and Sig data
func shortMsg(in *dns.Msg) *dns.Msg {
	for i := 0; i < len(in.Answer); i++ {
		in.Answer[i] = shortRR(in.Answer[i])
	}
	for i := 0; i < len(in.Ns); i++ {
		in.Ns[i] = shortRR(in.Ns[i])
	}
	for i := 0; i < len(in.Extra); i++ {
		in.Extra[i] = shortRR(in.Extra[i])
	}
	return in
}

func shortRR(r dns.RR) dns.RR {
	switch t := r.(type) {
	case *dns.RR_DS:
		t.Digest = "..."
	case *dns.RR_DNSKEY:
		t.PublicKey = "..."
	case *dns.RR_RRSIG:
		t.Signature = "..."
	case *dns.RR_NSEC3:
		t.Salt = "-" // Nobody cares
		if len(t.TypeBitMap) > 5 {
			t.TypeBitMap = t.TypeBitMap[1:5]
		}
	}
	return r
}
