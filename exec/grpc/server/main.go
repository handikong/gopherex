package main

import (
	"context"
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	helloworldv1 "gopherex.com/exec/grpc"
	"gopherex.com/exec/grpc/server/interceptor"
)

type greeterServer struct {
	helloworldv1.UnimplementedGreeterServer
}

func (s *greeterServer) SayHello(ctx context.Context, req *helloworldv1.HelloRequest) (*helloworldv1.HelloReply, error) {
	a := []int{1, 2, 3}
	fmt.Println(a[4])
	return &helloworldv1.HelloReply{Message: "Hello, " + req.GetName()}, nil
}

func main() {
	creds, err := credentials.NewServerTLSFromFile("./certs/server.crt", "./certs/server.key")
	if err != nil {
		log.Fatal(err)
	}
	grpcServer := grpc.NewServer(
		grpc.Creds(creds),
		grpc.UnaryInterceptor(
			interceptor.ErrorInterceptor(),
		),
	)
	listen, err := net.Listen("tcp", ":8085")
	defer listen.Close()
	if err != nil {
		log.Fatal(err)
	}
	helloworldv1.RegisterGreeterServer(grpcServer, &greeterServer{})
	// 启动服务器
	err = grpcServer.Serve(listen)
	if err != nil {
		defer grpcServer.Stop()
		log.Fatal(err)
	}
}
