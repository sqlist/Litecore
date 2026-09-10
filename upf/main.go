package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	upfpb "github.com/5g-core/proto/upf"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func main() {
	listenAddress := env("LISTEN_ADDRESS", ":50053")
	lis, err := net.Listen("tcp", listenAddress)
	if err != nil {
		log.Fatalf("UPF 监听失败: %v", err)
	}
	server := grpc.NewServer()
	handler := NewUPFHandler()
	upfpb.RegisterUPFServiceServer(server, handler)
	hs := health.NewServer()
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(server, hs)
	go func() {
		log.Printf("UPF 服务启动 listen=%s", listenAddress)
		if err := server.Serve(lis); err != nil {
			log.Fatalf("UPF 服务异常: %v", err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	hs.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	stopped := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(stopped)
	}()

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-stopped:
	case <-timer.C:
		log.Printf("UPF 优雅退出超过 5 秒，强制停止")
		server.Stop()
		<-stopped
	}
	handler.Close()
}
