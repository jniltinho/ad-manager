// Package nbfx converts XML to and from the .NET Binary Format ([MC-NBFX]) with the SOAP static dictionary
// ([MC-NBFS]), the "application/soap+msbin1" encoding the Azure AD Sync service speaks. It replaces python-wcfbin.
//
// Encode writes every text as a UTF-8 Chars record (never a typed record guessed from the text, which python-wcfbin
// does and which can change a value). Decode reads every record type of [MC-NBFX]; dynamic dictionary strings
// (the binary session encoding, application/soap+msbinsession1) are refused.
package nbfx

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// Limits applied by Decode.
const (
	MaxInput  = 16 << 20 // bytes of binary XML
	MaxOutput = 64 << 20 // bytes of XML text (dictionary strings and arrays expand)
	MaxDepth  = 256      // nested elements
)

// ErrInvalid wraps every Decode error caused by malformed input.
var ErrInvalid = errors.New("nbfx: invalid binary XML")

var dictIndex = func() map[string]int {
	m := make(map[string]int, len(staticDictionary))
	for i, s := range staticDictionary {
		m[s] = i * 2
	}
	return m
}()

// Record types used by Encode.
const (
	recEndElement          = 0x01
	recShortAttr           = 0x04
	recAttr                = 0x05
	recShortDictAttr       = 0x06
	recDictAttr            = 0x07
	recShortXmlns          = 0x08
	recXmlns               = 0x09
	recShortDictXmlns      = 0x0A
	recDictXmlns           = 0x0B
	recPrefixDictAttrA     = 0x0C
	recPrefixAttrA         = 0x26
	recShortElement        = 0x40
	recElement             = 0x41
	recShortDictElement    = 0x42
	recDictElement         = 0x43
	recPrefixDictElementA  = 0x44
	recPrefixElementA      = 0x5E
	recChars8Text          = 0x98
	recChars16Text         = 0x9A
	recChars32Text         = 0x9C
	recEmptyText           = 0xA8
	recFirstText, recLastT = 0x80, 0xBD
)

// Encode converts a well-formed XML document with one root element to binary XML. Text is kept as it is,
// whitespace included; only whitespace outside the root is dropped.
func Encode(doc []byte) ([]byte, error) {
	d := xml.NewDecoder(bytes.NewReader(doc))
	var out bytes.Buffer
	var text []byte // text of the current element not yet written
	pending := false
	flush := func(end bool) {
		if !pending {
			return
		}
		writeText(&out, string(text), end)
		text, pending = text[:0], false
	}
	var open []xml.Name
	roots := 0
	for {
		tok, err := d.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if len(open) == 0 {
				if roots++; roots > 1 {
					return nil, errors.New("nbfx: more than one root element")
				}
			}
			flush(false)
			open = append(open, t.Name)
			writeElement(&out, t.Name)
			seen := map[xml.Name]bool{}
			for _, a := range t.Attr {
				if seen[a.Name] {
					return nil, fmt.Errorf("nbfx: duplicate attribute %s", a.Name.Local)
				}
				seen[a.Name] = true
				writeAttr(&out, a)
			}
		case xml.EndElement:
			if len(open) == 0 || open[len(open)-1] != t.Name {
				return nil, fmt.Errorf("nbfx: unexpected </%s>", t.Name.Local)
			}
			open = open[:len(open)-1]
			if pending {
				flush(true) // the text record's "with end element" variant closes the element
			} else {
				out.WriteByte(recEndElement)
			}
		case xml.CharData:
			if len(open) == 0 {
				if len(bytes.TrimSpace(t)) != 0 {
					return nil, errors.New("nbfx: text outside the root element")
				}
				continue
			}
			text, pending = append(text, t...), true
		}
	}
	if len(open) != 0 || roots == 0 {
		return nil, errors.New("nbfx: no root element or an unclosed element")
	}
	return out.Bytes(), nil
}

func writeElement(out *bytes.Buffer, n xml.Name) {
	id, inDict := dictIndex[n.Local]
	switch p := n.Space; {
	case p == "" && inDict:
		out.WriteByte(recShortDictElement)
		writeInt31(out, id)
	case p == "":
		out.WriteByte(recShortElement)
		writeString(out, n.Local)
	case isLetterPrefix(p) && inDict:
		out.WriteByte(recPrefixDictElementA + p[0] - 'a')
		writeInt31(out, id)
	case isLetterPrefix(p):
		out.WriteByte(recPrefixElementA + p[0] - 'a')
		writeString(out, n.Local)
	case inDict:
		out.WriteByte(recDictElement)
		writeString(out, p)
		writeInt31(out, id)
	default:
		out.WriteByte(recElement)
		writeString(out, p)
		writeString(out, n.Local)
	}
}

func writeAttr(out *bytes.Buffer, a xml.Attr) {
	p, local := a.Name.Space, a.Name.Local
	if p == "" && local == "xmlns" {
		if id, ok := dictIndex[a.Value]; ok {
			out.WriteByte(recShortDictXmlns)
			writeInt31(out, id)
		} else {
			out.WriteByte(recShortXmlns)
			writeString(out, a.Value)
		}
		return
	}
	if p == "xmlns" {
		if id, ok := dictIndex[a.Value]; ok {
			out.WriteByte(recDictXmlns)
			writeString(out, local)
			writeInt31(out, id)
		} else {
			out.WriteByte(recXmlns)
			writeString(out, local)
			writeString(out, a.Value)
		}
		return
	}
	id, inDict := dictIndex[local]
	switch {
	case p == "" && inDict:
		out.WriteByte(recShortDictAttr)
		writeInt31(out, id)
	case p == "":
		out.WriteByte(recShortAttr)
		writeString(out, local)
	case isLetterPrefix(p) && inDict:
		out.WriteByte(recPrefixDictAttrA + p[0] - 'a')
		writeInt31(out, id)
	case isLetterPrefix(p):
		out.WriteByte(recPrefixAttrA + p[0] - 'a')
		writeString(out, local)
	case inDict:
		out.WriteByte(recDictAttr)
		writeString(out, p)
		writeInt31(out, id)
	default:
		out.WriteByte(recAttr)
		writeString(out, p)
		writeString(out, local)
	}
	writeText(out, a.Value, false)
}

func writeText(out *bytes.Buffer, s string, end bool) {
	var e byte
	if end {
		e = 1
	}
	switch n := len(s); {
	case n == 0:
		out.WriteByte(recEmptyText + e)
	case n <= math.MaxUint8:
		out.WriteByte(recChars8Text + e)
		out.WriteByte(byte(n))
	case n <= math.MaxUint16:
		out.WriteByte(recChars16Text + e)
		out.Write(binary.LittleEndian.AppendUint16(nil, uint16(n)))
	default:
		out.WriteByte(recChars32Text + e)
		out.Write(binary.LittleEndian.AppendUint32(nil, uint32(n)))
	}
	out.WriteString(s)
}

func isLetterPrefix(p string) bool { return len(p) == 1 && p[0] >= 'a' && p[0] <= 'z' }

func writeString(out *bytes.Buffer, s string) {
	writeInt31(out, len(s))
	out.WriteString(s)
}

// writeInt31 writes a MultiByteInt31: 7 bits per byte, low bits first, high bit set on all but the last byte.
func writeInt31(out *bytes.Buffer, v int) {
	for v >= 0x80 {
		out.WriteByte(byte(v) | 0x80)
		v >>= 7
	}
	out.WriteByte(byte(v))
}

// decoder reads records from b and writes XML to out.
type decoder struct {
	b     []byte
	pos   int
	out   bytes.Buffer
	open  []string // qualified names of the open elements
	roots int
	inTag bool            // a start tag is being written (attributes may follow)
	attrs map[string]bool // qualified names of the attributes of that start tag
	list  int             // >0 inside an element's StartListText; counts the items written
}

// Decode converts binary XML to a well-formed XML document with one root element. Input over MaxInput, output
// over MaxOutput, nesting over MaxDepth, invalid UTF-8, unpaired UTF-16 surrogates and characters XML does not
// allow are errors wrapping ErrInvalid.
func Decode(b []byte) (doc []byte, err error) {
	if len(b) > MaxInput {
		return nil, fmt.Errorf("%w: %d bytes, limit %d", ErrInvalid, len(b), MaxInput)
	}
	d := &decoder{b: b}
	for d.pos < len(d.b) {
		if err := d.record(); err != nil {
			return nil, fmt.Errorf("%w: offset %d: %v", ErrInvalid, d.pos, err)
		}
	}
	if len(d.open) != 0 || d.roots == 0 {
		return nil, fmt.Errorf("%w: truncated document", ErrInvalid)
	}
	return d.out.Bytes(), nil
}

func (d *decoder) record() error {
	t, err := d.byte()
	if err != nil {
		return err
	}
	if d.list > 0 && (t < recFirstText || t > recLastT) {
		return errors.New("only text records may appear in a list")
	}
	switch {
	case t == recEndElement:
		return d.endElement()
	case t == 0x02: // Comment: dropped, SOAP gives it no meaning
		_, err := d.string()
		return err
	case t == 0x03:
		return d.array()
	case t >= recShortAttr && t <= 0x3F:
		return d.attribute(t)
	case t >= recShortElement && t <= 0x77:
		return d.element(t)
	case t >= recFirstText && t <= recLastT:
		return d.text(t)
	}
	return fmt.Errorf("record type 0x%02X not supported", t)
}

func (d *decoder) element(t byte) error {
	var prefix, name string
	var err error
	switch {
	case t == recShortElement:
		name, err = d.string()
	case t == recElement:
		if prefix, err = d.string(); err == nil {
			name, err = d.string()
		}
	case t == recShortDictElement:
		name, err = d.dictString()
	case t == recDictElement:
		if prefix, err = d.string(); err == nil {
			name, err = d.dictString()
		}
	case t >= recPrefixDictElementA && t < recPrefixElementA:
		prefix = string(rune('a' + t - recPrefixDictElementA))
		name, err = d.dictString()
	default:
		prefix = string(rune('a' + t - recPrefixElementA))
		name, err = d.string()
	}
	if err != nil {
		return err
	}
	if len(d.open) >= MaxDepth {
		return fmt.Errorf("nesting deeper than %d", MaxDepth)
	}
	if len(d.open) == 0 {
		if d.roots++; d.roots > 1 {
			return errors.New("more than one root element")
		}
	}
	qn, err := qname(prefix, name)
	if err != nil {
		return err
	}
	if err := d.closeTag(); err != nil {
		return err
	}
	d.open = append(d.open, qn)
	d.inTag = true
	clear(d.attrs)
	return d.emit("<" + qn)
}

func (d *decoder) attribute(t byte) error {
	if !d.inTag {
		return errors.New("attribute outside a start tag")
	}
	var prefix, name, value string
	var err error
	switch {
	case t == recShortAttr:
		name, err = d.string()
	case t == recAttr:
		if prefix, err = d.string(); err == nil {
			name, err = d.string()
		}
	case t == recShortDictAttr:
		name, err = d.dictString()
	case t == recDictAttr:
		if prefix, err = d.string(); err == nil {
			name, err = d.dictString()
		}
	case t == recShortXmlns:
		name = "xmlns"
		value, err = d.string()
	case t == recXmlns:
		prefix = "xmlns"
		if name, err = d.string(); err == nil {
			value, err = d.string()
		}
	case t == recShortDictXmlns:
		name = "xmlns"
		value, err = d.dictString()
	case t == recDictXmlns:
		prefix = "xmlns"
		if name, err = d.string(); err == nil {
			value, err = d.dictString()
		}
	case t >= recPrefixDictAttrA && t < recPrefixAttrA:
		prefix = string(rune('a' + t - recPrefixDictAttrA))
		name, err = d.dictString()
	default:
		prefix = string(rune('a' + t - recPrefixAttrA))
		name, err = d.string()
	}
	if err != nil {
		return err
	}
	qn, err := qname(prefix, name)
	if err != nil {
		return err
	}
	if d.attrs == nil {
		d.attrs = map[string]bool{}
	}
	if d.attrs[qn] {
		return fmt.Errorf("duplicate attribute %s", qn)
	}
	d.attrs[qn] = true
	if err := d.emit(" " + qn + `="`); err != nil {
		return err
	}
	if t >= recShortXmlns && t <= recDictXmlns {
		err = d.emitText(value)
	} else {
		err = d.attributeValue()
	}
	if err != nil {
		return err
	}
	return d.emit(`"`)
}

// attributeValue reads one text record, or a StartListText … EndListText whose items it writes separated by
// spaces, straight to the output.
func (d *decoder) attributeValue() error {
	t, err := d.byte()
	if err != nil {
		return err
	}
	if t != 0xA4 {
		if !scalarText(t) {
			return fmt.Errorf("attribute value record 0x%02X", t)
		}
		v, err := d.textValue(t)
		if err != nil {
			return err
		}
		return d.emitText(v)
	}
	for n := 0; ; n++ {
		it, err := d.byte()
		if err != nil {
			return err
		}
		if it == 0xA6 {
			return nil
		}
		if !scalarText(it) {
			return fmt.Errorf("list item record 0x%02X", it)
		}
		v, err := d.textValue(it)
		if err != nil {
			return err
		}
		if n > 0 {
			if err := d.emit(" "); err != nil {
				return err
			}
		}
		if err := d.emitText(v); err != nil {
			return err
		}
	}
}

// scalarText reports whether t is a text record without the end-element bit and is not a list marker.
func scalarText(t byte) bool {
	return t >= recFirstText && t <= recLastT && t&1 == 0 && t != 0xA4 && t != 0xA6
}

func (d *decoder) text(t byte) error {
	if len(d.open) == 0 {
		return errors.New("text outside an element")
	}
	base, end := t&^1, t&1 == 1
	switch base {
	case 0xA4: // StartListText
		if end || d.list != 0 {
			return errors.New("bad list")
		}
		d.list = 1
		return d.closeTag()
	case 0xA6: // EndListText
		if end || d.list == 0 {
			return errors.New("bad list")
		}
		d.list = 0
		return nil
	}
	if end && d.list != 0 {
		return errors.New("end element inside a list")
	}
	v, err := d.textValue(base)
	if err != nil {
		return err
	}
	if err := d.closeTag(); err != nil {
		return err
	}
	if d.list > 0 {
		if d.list > 1 {
			if err := d.emit(" "); err != nil {
				return err
			}
		}
		d.list++
	}
	if err := d.emitText(v); err != nil {
		return err
	}
	if end {
		return d.endElement()
	}
	return nil
}

// array reads an Array record: an element record with its attributes, an EndElement, the item record type, a
// count and the items, and writes one element per item.
func (d *decoder) array() error {
	if err := d.closeTag(); err != nil {
		return err
	}
	t, err := d.byte()
	if err != nil {
		return err
	}
	if t < recShortElement || t > 0x77 {
		return fmt.Errorf("array element record 0x%02X", t)
	}
	start, depth, roots := d.out.Len(), len(d.open), d.roots
	if err := d.element(t); err != nil {
		return err
	}
	for {
		a, err := d.byte()
		if err != nil {
			return err
		}
		if a == recEndElement {
			break
		}
		if a < recShortAttr || a > 0x3F {
			return fmt.Errorf("array attribute record 0x%02X", a)
		}
		if err := d.attribute(a); err != nil {
			return err
		}
	}
	tag := string(d.out.Bytes()[start:]) + ">"
	name := d.open[len(d.open)-1]
	d.out.Truncate(start)
	d.open, d.inTag, d.roots = d.open[:depth], false, roots
	vt, err := d.byte()
	if err != nil {
		return err
	}
	switch vt { // the "with end element" variants of the fixed-size types ([MC-NBFX] 2.2.3.31)
	case 0xB5, 0x8B, 0x8D, 0x8F, 0x91, 0x93, 0x95, 0x97, 0xAF, 0xB1:
	default:
		return fmt.Errorf("array item record 0x%02X", vt)
	}
	n, err := d.int31()
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("empty array")
	}
	for i := 0; i < n; i++ {
		if len(d.open) == 0 {
			if d.roots++; d.roots > 1 {
				return errors.New("more than one root element")
			}
		}
		v, err := d.textValue(vt &^ 1)
		if err != nil {
			return err
		}
		if err := d.emit(tag); err != nil {
			return err
		}
		if err := d.emitText(v); err != nil {
			return err
		}
		if err := d.emit("</" + name + ">"); err != nil {
			return err
		}
	}
	return nil
}

// textValue reads the payload of text record base type t (the "with end element" bit cleared).
func (d *decoder) textValue(t byte) (string, error) {
	switch t {
	case 0x80:
		return "0", nil
	case 0x82:
		return "1", nil
	case 0x84:
		return "false", nil
	case 0x86:
		return "true", nil
	case 0x88:
		b, err := d.take(1)
		if err != nil {
			return "", err
		}
		return strconv.Itoa(int(int8(b[0]))), nil
	case 0x8A:
		b, err := d.take(2)
		if err != nil {
			return "", err
		}
		return strconv.Itoa(int(int16(binary.LittleEndian.Uint16(b)))), nil
	case 0x8C:
		b, err := d.take(4)
		if err != nil {
			return "", err
		}
		return strconv.Itoa(int(int32(binary.LittleEndian.Uint32(b)))), nil
	case 0x8E:
		b, err := d.take(8)
		if err != nil {
			return "", err
		}
		return strconv.FormatInt(int64(binary.LittleEndian.Uint64(b)), 10), nil
	case 0xB2:
		b, err := d.take(8)
		if err != nil {
			return "", err
		}
		return strconv.FormatUint(binary.LittleEndian.Uint64(b), 10), nil
	case 0x90:
		b, err := d.take(4)
		if err != nil {
			return "", err
		}
		return formatFloat(float64(math.Float32frombits(binary.LittleEndian.Uint32(b))), 32), nil
	case 0x92:
		b, err := d.take(8)
		if err != nil {
			return "", err
		}
		return formatFloat(math.Float64frombits(binary.LittleEndian.Uint64(b)), 64), nil
	case 0x94:
		b, err := d.take(16)
		if err != nil {
			return "", err
		}
		return formatDecimal(b)
	case 0x96:
		b, err := d.take(8)
		if err != nil {
			return "", err
		}
		return formatDateTime(binary.LittleEndian.Uint64(b))
	case 0x98, 0x9A, 0x9C:
		n, err := d.length(t, 0x98)
		if err != nil {
			return "", err
		}
		b, err := d.take(n)
		return string(b), err
	case 0x9E, 0xA0, 0xA2:
		n, err := d.length(t, 0x9E)
		if err != nil {
			return "", err
		}
		b, err := d.take(n)
		return base64.StdEncoding.EncodeToString(b), err
	case 0xA8:
		return "", nil
	case 0xAA:
		return d.dictString()
	case 0xAC, 0xB0:
		b, err := d.take(16)
		if err != nil {
			return "", err
		}
		g := formatGUID(b)
		if t == 0xAC {
			return "urn:uuid:" + g, nil
		}
		return g, nil
	case 0xAE:
		b, err := d.take(8)
		if err != nil {
			return "", err
		}
		return formatTimeSpan(int64(binary.LittleEndian.Uint64(b))), nil
	case 0xB4:
		b, err := d.take(1)
		if err != nil {
			return "", err
		}
		switch b[0] {
		case 0:
			return "false", nil
		case 1:
			return "true", nil
		}
		return "", fmt.Errorf("bool value %d", b[0])
	case 0xB6, 0xB8, 0xBA:
		n, err := d.length(t, 0xB6)
		if err != nil {
			return "", err
		}
		b, err := d.take(n)
		if err != nil {
			return "", err
		}
		if n%2 != 0 {
			return "", errors.New("odd UTF-16 length")
		}
		u := make([]uint16, n/2)
		for i := range u {
			u[i] = binary.LittleEndian.Uint16(b[2*i:])
		}
		for i := 0; i < len(u); i++ {
			switch {
			case utf16.IsSurrogate(rune(u[i])) && u[i] < 0xDC00 && i+1 < len(u) && u[i+1] >= 0xDC00 && u[i+1] <= 0xDFFF:
				i++ // a high surrogate followed by a low one
			case utf16.IsSurrogate(rune(u[i])):
				return "", errors.New("unpaired UTF-16 surrogate")
			}
		}
		return string(utf16.Decode(u)), nil
	case 0xBC:
		p, err := d.byte()
		if err != nil {
			return "", err
		}
		if p > 25 {
			return "", fmt.Errorf("QName prefix %d", p)
		}
		s, err := d.dictString()
		return string(rune('a'+p)) + ":" + s, err
	}
	return "", fmt.Errorf("text record 0x%02X not supported", t)
}

// length reads the 1-, 2- or 4-byte length of a Chars, Bytes or UnicodeChars record (t-base is 0, 2 or 4).
func (d *decoder) length(t, base byte) (int, error) {
	switch t - base {
	case 0:
		b, err := d.take(1)
		if err != nil {
			return 0, err
		}
		return int(b[0]), nil
	case 2:
		b, err := d.take(2)
		if err != nil {
			return 0, err
		}
		return int(binary.LittleEndian.Uint16(b)), nil
	}
	b, err := d.take(4)
	if err != nil {
		return 0, err
	}
	n := int32(binary.LittleEndian.Uint32(b))
	if n < 0 {
		return 0, errors.New("negative length")
	}
	return int(n), nil
}

func (d *decoder) endElement() error {
	if len(d.open) == 0 {
		return errors.New("end element without an open element")
	}
	if d.list != 0 {
		return errors.New("end element inside a list")
	}
	if err := d.closeTag(); err != nil {
		return err
	}
	name := d.open[len(d.open)-1]
	d.open = d.open[:len(d.open)-1]
	return d.emit("</" + name + ">")
}

func (d *decoder) closeTag() error {
	if !d.inTag {
		return nil
	}
	d.inTag = false
	return d.emit(">")
}

// emit writes markup, within MaxOutput.
func (d *decoder) emit(s string) error {
	if d.out.Len()+len(s) > MaxOutput {
		return fmt.Errorf("XML output over %d bytes", MaxOutput)
	}
	d.out.WriteString(s)
	return nil
}

// emitText writes v escaped, after checking it holds only characters XML allows and that its escaped length fits
// within MaxOutput, so the buffer never grows past it.
func (d *decoder) emitText(v string) error {
	if err := validText(v); err != nil {
		return err
	}
	if d.out.Len()+escapedLen(v) > MaxOutput {
		return fmt.Errorf("XML output over %d bytes", MaxOutput)
	}
	return xml.EscapeText(&d.out, []byte(v))
}

// escapedLen is the length of v after xml.EscapeText.
func escapedLen(v string) int {
	n := len(v)
	for i := 0; i < len(v); i++ {
		switch v[i] {
		case '<', '>':
			n += 3 // &lt; &gt;
		case '"', '\'', '&', '\t', '\n', '\r':
			n += 4 // &#34; &#39; &amp; &#x9; &#xA; &#xD;
		}
	}
	return n
}

// validText refuses invalid UTF-8 and characters outside the XML 1.0 Char production, which escaping would
// otherwise replace silently.
func validText(s string) error {
	for i, r := range s {
		if r == utf8.RuneError && !strings.HasPrefix(s[i:], "\uFFFD") {
			return errors.New("invalid UTF-8 in text")
		}
		if !(r == 0x09 || r == 0x0A || r == 0x0D || r >= 0x20 && r <= 0xD7FF || r >= 0xE000 && r <= 0xFFFD ||
			r >= 0x10000 && r <= 0x10FFFF) {
			return fmt.Errorf("character U+%04X not allowed in XML", r)
		}
	}
	return nil
}

func (d *decoder) byte() (byte, error) {
	b, err := d.take(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

// take returns the next n bytes; the length is checked against the input before anything is allocated.
func (d *decoder) take(n int) ([]byte, error) {
	if n < 0 || n > len(d.b)-d.pos {
		return nil, io.ErrUnexpectedEOF
	}
	b := d.b[d.pos : d.pos+n]
	d.pos += n
	return b, nil
}

func (d *decoder) int31() (int, error) {
	v := 0
	for i := 0; i < 5; i++ {
		c, err := d.byte()
		if err != nil {
			return 0, err
		}
		if i == 4 && c > 0x07 {
			return 0, errors.New("MultiByteInt31 overflow")
		}
		v |= int(c&0x7F) << (7 * i)
		if c&0x80 == 0 {
			return v, nil
		}
	}
	return 0, errors.New("MultiByteInt31 too long")
}

func (d *decoder) string() (string, error) {
	n, err := d.int31()
	if err != nil {
		return "", err
	}
	b, err := d.take(n)
	return string(b), err
}

func (d *decoder) dictString() (string, error) {
	id, err := d.int31()
	if err != nil {
		return "", err
	}
	if id%2 == 1 {
		return "", errors.New("dynamic dictionary string (binary session encoding) not supported")
	}
	if id/2 >= len(staticDictionary) {
		return "", fmt.Errorf("dictionary string %d unknown", id)
	}
	return staticDictionary[id/2], nil
}

// qname validates a name read from the input so it cannot inject markup into the XML output.
func qname(prefix, name string) (string, error) {
	if !isNCName(name) || (prefix != "" && !isNCName(prefix)) {
		return "", fmt.Errorf("invalid XML name %q:%q", prefix, name)
	}
	if prefix == "" {
		return name, nil
	}
	return prefix + ":" + name, nil
}

// isNCName accepts an ASCII subset of XML NCNames (the service's names are ASCII): a letter or '_', then letters,
// digits, '.', '-' or '_'.
func isNCName(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		letter := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
		if !letter && (i == 0 || !(c >= '0' && c <= '9' || c == '.' || c == '-')) {
			return false
		}
	}
	return s != ""
}

func formatFloat(v float64, bits int) string {
	switch {
	case math.IsInf(v, 1):
		return "INF"
	case math.IsInf(v, -1):
		return "-INF"
	case math.IsNaN(v):
		return "NaN"
	}
	return strconv.FormatFloat(v, 'G', -1, bits)
}

// formatDecimal formats a .NET System.Decimal: 2 reserved bytes, scale, sign, 32-bit high, 64-bit low.
func formatDecimal(b []byte) (string, error) {
	scale := int(b[2])
	if scale > 28 {
		return "", fmt.Errorf("decimal scale %d", scale)
	}
	v := new(big.Int).SetUint64(uint64(binary.LittleEndian.Uint32(b[4:8])))
	v.Lsh(v, 64).Or(v, new(big.Int).SetUint64(binary.LittleEndian.Uint64(b[8:16])))
	s := v.String()
	if scale > 0 {
		if len(s) <= scale {
			s = strings.Repeat("0", scale-len(s)+1) + s
		}
		s = s[:len(s)-scale] + "." + s[len(s)-scale:]
	}
	if b[3]&0x80 != 0 && v.Sign() != 0 {
		s = "-" + s
	}
	return s, nil
}

// ticksEpoch is 0001-01-01 in Unix seconds; a .NET tick is 100 ns.
const ticksEpoch = -62135596800

func formatDateTime(v uint64) (string, error) {
	kind, ticks := v>>62, int64(v&0x3FFFFFFFFFFFFFFF)
	if ticks > 3155378975999999999 { // 9999-12-31T23:59:59.9999999
		return "", errors.New("date out of range")
	}
	if kind == 3 {
		return "", errors.New("DateTime kind 3 is reserved")
	}
	t := time.Unix(ticksEpoch+ticks/1e7, (ticks%1e7)*100).UTC()
	s := t.Format("2006-01-02T15:04:05")
	if f := ticks % 1e7; f != 0 {
		s += "." + strings.TrimRight(fmt.Sprintf("%07d", f), "0")
	}
	switch kind {
	case 1: // UTC
		s += "Z"
	case 2: // local time of the writer: shown with this host's offset at that date, as .NET's reader does
		_, off := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.Local).Zone()
		sign := '+'
		if off < 0 {
			sign, off = '-', -off
		}
		s += fmt.Sprintf("%c%02d:%02d", sign, off/3600, off/60%60)
	}
	return s, nil
}

// formatTimeSpan formats ticks as an xs:duration, as XmlConvert.ToString(TimeSpan) does.
func formatTimeSpan(ticks int64) string {
	var sb strings.Builder
	u := uint64(ticks)
	if ticks < 0 {
		sb.WriteByte('-')
		u = uint64(-ticks)
	}
	sb.WriteByte('P')
	days, rem := u/864e9, u%864e9
	if days > 0 {
		sb.WriteString(strconv.FormatUint(days, 10) + "D")
	}
	if rem > 0 || days == 0 {
		sb.WriteByte('T')
		h, m, s, f := rem/36e9, rem/6e8%60, rem/1e7%60, rem%1e7
		if h > 0 {
			sb.WriteString(strconv.FormatUint(h, 10) + "H")
		}
		if m > 0 {
			sb.WriteString(strconv.FormatUint(m, 10) + "M")
		}
		if s > 0 || f > 0 || (h == 0 && m == 0) {
			sb.WriteString(strconv.FormatUint(s, 10))
			if f > 0 {
				sb.WriteString("." + strings.TrimRight(fmt.Sprintf("%07d", f), "0"))
			}
			sb.WriteByte('S')
		}
	}
	return sb.String()
}

// formatGUID formats 16 bytes in the .NET (mixed-endian) GUID layout.
func formatGUID(b []byte) string {
	return fmt.Sprintf("%08x-%04x-%04x-%x-%x", binary.LittleEndian.Uint32(b[0:4]),
		binary.LittleEndian.Uint16(b[4:6]), binary.LittleEndian.Uint16(b[6:8]), b[8:10], b[10:16])
}
