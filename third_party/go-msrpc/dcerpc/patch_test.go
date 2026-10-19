package dcerpc

import (
	"context"
	"errors"
	"testing"

	"github.com/oiweiwei/go-msrpc/ndr"
)

// retOp is an operation whose response is one uint32 (as the Return of UpdateRecord2).
type retOp struct{ Return uint32 }

func (*retOp) OpNum() int                                            { return 0 }
func (*retOp) OpName() string                                        { return "ret" }
func (*retOp) MarshalNDRRequest(context.Context, ndr.Writer) error   { return nil }
func (*retOp) UnmarshalNDRRequest(context.Context, ndr.Reader) error { return nil }
func (*retOp) MarshalNDRResponse(context.Context, ndr.Writer) error  { return nil }
func (o *retOp) UnmarshalNDRResponse(_ context.Context, r ndr.Reader) error {
	return r.ReadData(&o.Return)
}

// TestBodyComplete: an answer cut short (here: no stub at all, as a forged auth_pad_length leaves it) is an error,
// not an operation with zero values; the full answer passes (ad-manager patch).
func TestBodyComplete(t *testing.T) {
	p := NewPresentation(nil) // NDR 2.0 transfer syntax
	for _, tt := range []struct {
		name string
		stub []byte
		ok   bool
	}{
		{"full", []byte{0xf2, 0x25, 0, 0}, true},
		{"cut to nothing", nil, false},
		{"cut short", []byte{0xf2, 0x25}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			op := &retOp{}
			body := NewBody(context.Background(), op, p, false)
			defer body.Close()
			_, _ = body.DecodeFrom(tt.stub, ndr.DefaultDataRepresentation, len(tt.stub))
			err := bodyComplete(body)
			if (err == nil) != tt.ok || (!tt.ok && !errors.Is(err, ErrTruncatedResponse)) {
				t.Fatalf("bodyComplete = %v (Return %#x)", err, op.Return)
			}
			if tt.ok && op.Return != 0x25f2 {
				t.Fatalf("Return %#x", op.Return)
			}
		})
	}
}
