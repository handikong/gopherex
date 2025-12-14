package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
	helloworldv1 "gopherex.com/exec/grpc"
)

func main() {
	creds, _ := credentials.NewClientTLSFromFile("./certs/ca.crt", "localhost")
	conn, err := grpc.NewClient(
		":8085",
		grpc.WithTransportCredentials(creds),
	)
	if err != nil {
		log.Fatal(err)
	}
	//defer conn.Close()
	c := helloworldv1.NewGreeterClient(conn)

	rpcCtx, rpcCancel := context.WithTimeout(context.Background(), time.Second)
	defer rpcCancel()

	resp, err := c.SayHello(rpcCtx, &helloworldv1.HelloRequest{Name: "GopherX2"})
	grpcStatus, _ := status.FromError(err)
	fmt.Println(grpcStatus.Code())
	fmt.Println(grpcStatus.Message())
	fmt.Println(grpcStatus.Details())
	if err != nil {
		log.Fatalf("call: %v", err)
	}

	log.Println("resp:", resp.GetMessage())

}
