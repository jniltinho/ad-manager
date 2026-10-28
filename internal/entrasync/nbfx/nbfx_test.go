package nbfx

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Vectors from python-wcfbin's doctests and [MC-NBFX] record layouts.
func TestDecodeVectors(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"5602450e0101", "<s:Envelope><b:Body></b:Body></s:Envelope>"},
		{"4101610474657374" + "01", "<a:test></a:test>"},
	} {
		got, err := Decode(mustHex(t, tc.in))
		if err != nil || string(got) != tc.want {
			t.Errorf("Decode(%s) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}

func TestDecodeTextRecords(t *testing.T) {
	el := "4001" + "61" // <a>
	for _, tc := range []struct{ rec, want string }{
		{"81", "0"},
		{"83", "1"},
		{"85", "false"},
		{"87", "true"},
		{"89ff", "-1"},
		{"8b3905", "1337"},
		{"b501", "true"},
		{"99" + "03616263", "abc"},
		{"b7" + "06610062006300", "abc"},
		{"9f" + "03010203", "AQID"},
		{"a9", ""},
		{"ab" + "02", "Envelope"},
		{"ad" + "00112233445566778899aabbccddeeff", "urn:uuid:33221100-5544-7766-8899-aabbccddeeff"},
		{"b1" + "00112233445566778899aabbccddeeff", "33221100-5544-7766-8899-aabbccddeeff"},
		{"95" + "0000030000000000" + "3905000000000000", "1.337"},
		{"95" + "0000038000000000" + "0500000000000000", "-0.005"},
		{"97" + "00408ef95b47c808", "2006-05-17T00:00:00"},
		{"97" + "00408ef95b47c848", "2006-05-17T00:00:00Z"},
		{"af" + "0051250200000000", "PT3.6S"}, // 36,000,000 ticks of 100 ns
		{"bd" + "0102", "b:Envelope"},
		{"93" + "31 08 ac 1c 5a 64 f5 3f", "1.337"},
		{"a4" + "8802" + "8803" + "a6" + "01", "2 3"},
	} {
		got, err := Decode(mustHex(t, el+tc.rec))
		if err != nil {
			t.Errorf("record %s: %v", tc.rec, err)
			continue
		}
		if want := "<a>" + tc.want + "</a>"; string(got) != want {
			t.Errorf("record %s = %q; want %q", tc.rec, got, want)
		}
	}
}

func TestEncodeVectors(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"<s:Envelope><b:Body /></s:Envelope>", "5602450e0101"},
		{"<a:test></a:test>", "5e04746573740 1"},
		{`<qq x="1">hi</qq>`, "40027171" + "040178" + "980131" + "9902" + "6869"},
		{`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"/>`, "5602" + "0b0173" + "04" + "01"},
		{`<doc xmlns="urn:x"/>`, "4003646f63" + "0805" + "75726e3a78" + "01"},
	} {
		got, err := Encode([]byte(tc.in))
		want := strings.ReplaceAll(tc.want, " ", "")
		if err != nil || hex.EncodeToString(got) != want {
			t.Errorf("Encode(%s) = %x, %v; want %s", tc.in, got, err, want)
		}
	}
}

// The codec must carry any value unchanged, where python-wcfbin's typed records would alter it.
func TestRoundTripKeepsValues(t *testing.T) {
	values := []string{"0612345678", "test", "True", "Sales  ", " ", "R&D <Lab>", "</c:Value><c:Key>x",
		"abd=", "Envelope", "b:Envelope", "urn:uuid:33221100-5544-7766-8899-aabbccddeeff", "ção 😀",
		strings.Repeat("x", 300), strings.Repeat("y", 70000)}
	for _, v := range values {
		var doc bytes.Buffer
		doc.WriteString(`<c:Value i:type="d:string" xmlns:d="http://www.w3.org/2001/XMLSchema">`)
		_ = xml.EscapeText(&doc, []byte(v))
		doc.WriteString(`</c:Value>`)
		bin, err := Encode(doc.Bytes())
		if err != nil {
			t.Fatalf("Encode(%q): %v", v, err)
		}
		back, err := Decode(bin)
		if err != nil {
			t.Fatalf("Decode(%q): %v", v, err)
		}
		var got struct {
			Text string `xml:",chardata"`
			Type string `xml:"type,attr"`
		}
		if err := xml.Unmarshal(back, &got); err != nil {
			t.Fatalf("xml of %q: %v (%s)", v, err, back)
		}
		if got.Text != v || got.Type != "d:string" {
			t.Errorf("value %q came back as %q (type %q)", v, got.Text, got.Type)
		}
	}
}

// Examples from [MC-NBFX] and the Codex review: lists in attributes, arrays, dates with a time zone.
func TestDecodeSpecExamples(t *testing.T) {
	time.Local = time.FixedZone("BRT", -3*3600)
	for _, tc := range []struct{ in, want string }{
		{"4003646f63040161a4887b980568656c6c6f86a601", `<doc a="123 hello true"></doc>`},
		{"400161" + "03" + "4003617272" + "01" + "8b03" + "3333" + "8888" + "dddd" + "01",
			"<a><arr>13107</arr><arr>-30584</arr><arr>-8739</arr></a>"},
		{"400161" + "97" + "00408ef95b47c888", "<a>2006-05-17T00:00:00-03:00</a>"},
		{"400161" + "99" + "03" + "200a20", "<a> &#xA; </a>"},
	} {
		got, err := Decode(mustHex(t, tc.in))
		if err != nil || string(got) != tc.want {
			t.Errorf("Decode(%s) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}

func TestEncodeRejectsMalformedXML(t *testing.T) {
	for _, in := range []string{"<a></b>", "<a/><b/>", "text<a/>", "", "<a>", `<a x="1" x="2"/>`,
		`<a xmlns:p="u" xmlns:p="v"/>`} {
		if _, err := Encode([]byte(in)); err == nil {
			t.Errorf("Encode(%q) accepted", in)
		}
	}
	if got, err := Encode([]byte("<a>\n  </a>")); err != nil || hex.EncodeToString(got) != "42b601"+"9903"+"0a2020" {
		t.Errorf("whitespace value: %x, %v", got, err)
	}
}

// A 16 MiB attribute list of empty items decodes with memory in proportion to its output (one string per item
// cost at least 256 MiB before).
func TestDecodeListMemory(t *testing.T) {
	in := append(mustHex(t, "400161040162a4"), bytes.Repeat([]byte{0xA8}, MaxInput-9)...)
	in = append(in, 0xA6, 0x01)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	doc, err := Decode(in)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 8*uint64(len(doc)) {
		t.Fatalf("decoding %d bytes of XML allocated %d MiB", len(doc), alloc>>20)
	}
}

// A plain value close to the output limit decodes: the quota counts the escaped length, not a worst case.
func TestDecodeLargePlainText(t *testing.T) {
	v := bytes.Repeat([]byte("x"), 11<<20)
	in := append(mustHex(t, "4001619d"), binary.LittleEndian.AppendUint32(nil, uint32(len(v)))...)
	if got, err := Decode(append(in, v...)); err != nil || len(got) != len(v)+len("<a></a>") {
		t.Fatalf("11 MiB value: %d bytes, %v", len(got), err)
	}
}

func TestRoundTripEnvelope(t *testing.T) {
	in := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:a="http://www.w3.org/2005/08/addressing">
	<s:Header><a:Action s:mustUnderstand="1">http://schemas.microsoft.com/online/aws/change/2010/01/IProvisioningWebService/ProvisionCredentials</a:Action>
	<a:MessageID>urn:uuid:33221100-5544-7766-8899-aabbccddeeff</a:MessageID><a:ReplyTo><a:Address>http://www.w3.org/2005/08/addressing/anonymous</a:Address></a:ReplyTo>
	</s:Header><s:Body><ProvisionCredentials xmlns="http://schemas.microsoft.com/online/aws/change/2010/01"><b:Empty i:nil="true"/></ProvisionCredentials></s:Body></s:Envelope>`
	bin, err := Encode([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	back, err := Decode(bin)
	if err != nil {
		t.Fatal(err)
	}
	if canon(t, []byte(in)) != canon(t, back) {
		t.Errorf("round trip differs:\n%s\n%s", canon(t, []byte(in)), canon(t, back))
	}
}

func TestDecodeRejectsHostileInput(t *testing.T) {
	deep := bytes.Repeat([]byte{0x40, 0x01, 'a'}, MaxDepth+1)
	for name, in := range map[string][]byte{
		"truncated element":   mustHex(t, "4005616263"),
		"length past input":   mustHex(t, "400161"+"9cffffff7f"),
		"negative length":     mustHex(t, "400161"+"9d00000080"),
		"unclosed":            mustHex(t, "400161"),
		"too deep":            deep,
		"too big":             make([]byte, MaxInput+1),
		"array":               mustHex(t, "03"),
		"dynamic dict":        mustHex(t, "4201"),
		"unknown dict":        mustHex(t, "42"+"feff03"),
		"markup in name":      mustHex(t, "4003"+"613e62"+"01"),
		"end without start":   mustHex(t, "01"),
		"text at top level":   mustHex(t, "99016101"),
		"int31 overflow":      mustHex(t, "40ffffffff7f"),
		"bad bool":            mustHex(t, "400161"+"b502"),
		"attr outside tag":    mustHex(t, "400161"+"980161"+"040162"+"9800"),
		"two roots":           mustHex(t, "400161"+"01"+"400162"+"01"),
		"element in list":     mustHex(t, "400161"+"a4"+"400162"),
		"end inside list":     mustHex(t, "400161"+"a4"+"8902"),
		"list item record":    mustHex(t, "40016104016261"+"a4"+"a4"),
		"invalid UTF-8":       mustHex(t, "4001619901ff"),
		"control character":   mustHex(t, "400161990101"),
		"lone surrogate":      mustHex(t, "400161"+"b702"+"00d8"),
		"DateTime kind 3":     mustHex(t, "400161"+"97"+"00408ef95b47c8c8"),
		"array of chars":      mustHex(t, "400161"+"03"+"400162"+"01"+"9901"+"61"),
		"amplification":       amplification(),
		"duplicate attribute": mustHex(t, "40016104017898013104017898013201"),
		"duplicate xmlns":     mustHex(t, "400161"+"090170"+"0175"+"090170"+"0176"+"01"),
		"empty array":         mustHex(t, "40016103400162018b0001"),
	} {
		if _, err := Decode(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v; want ErrInvalid", name, err)
		}
	}
}

func TestDictionary(t *testing.T) {
	if len(staticDictionary) != 487 || staticDictionary[1] != "Envelope" || staticDictionary[0x3CC/2] != "detail" {
		t.Fatalf("static dictionary: %d entries", len(staticDictionary))
	}
}

func FuzzDecode(f *testing.F) {
	f.Add(mustHex(f, "5602450e0101"))
	f.Add(mustHex(f, "400161"+"a4"+"8802"+"a6"+"01"))
	f.Fuzz(func(t *testing.T, b []byte) {
		doc, err := Decode(b)
		if err != nil {
			return
		}
		// Whatever Decode accepts is one well-formed document that survives a round trip unchanged.
		bin, err := Encode(doc)
		if err != nil {
			t.Fatalf("Encode(Decode(%x)) = %v (%s)", b, err, doc)
		}
		again, err := Decode(bin)
		if err != nil || !bytes.Equal(again, doc) {
			t.Fatalf("round trip of %x changed:\n%s\n%s (%v)", b, doc, again, err)
		}
	})
}

// amplification is a valid-looking input of MaxInput bytes whose dictionary strings expand past MaxOutput.
func amplification() []byte {
	b := []byte{0x40, 0x01, 'a'}
	for len(b) < MaxInput-2 {
		b = append(b, 0xAA, 0x66)
	}
	return append(b, 0x01)
}

func canon(t *testing.T, doc []byte) string {
	t.Helper()
	d := xml.NewDecoder(bytes.NewReader(doc))
	var sb strings.Builder
	for {
		tok, err := d.RawToken()
		if err != nil {
			break
		}
		switch v := tok.(type) {
		case xml.StartElement:
			sb.WriteString("<" + v.Name.Space + ":" + v.Name.Local)
			for _, a := range v.Attr {
				sb.WriteString(" " + a.Name.Space + ":" + a.Name.Local + "=" + a.Value)
			}
			sb.WriteString(">")
		case xml.EndElement:
			sb.WriteString("</>")
		case xml.CharData:
			if s := strings.TrimSpace(string(v)); s != "" {
				sb.WriteString(s)
			}
		}
	}
	return sb.String()
}

func mustHex(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}
