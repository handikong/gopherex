package main

import (
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gopherex.com/pkg/xerr"
)

func TestMain(t *testing.T) {
	err := xerr.New(codes.ResourceExhausted, "rate limited")
	st, ok := status.FromError(err)
	fmt.Println("ok:", ok, "code:", st.Code(), "msg:", st.Message())
}
