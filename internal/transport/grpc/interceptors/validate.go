package interceptors

import (
	"buf.build/go/protovalidate"
	protovalidate_mw "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/protovalidate"
	"google.golang.org/grpc"
)

type Validator struct {
	v protovalidate.Validator
}

func NewValidator() (*Validator, error) {
	v, err := protovalidate.New()
	if err != nil {
		return nil, err
	}
	return &Validator{v: v}, nil
}

func (v *Validator) Unary() grpc.UnaryServerInterceptor {
	return protovalidate_mw.UnaryServerInterceptor(v.v)
}

func (v *Validator) Stream() grpc.StreamServerInterceptor {
	return protovalidate_mw.StreamServerInterceptor(v.v)
}
