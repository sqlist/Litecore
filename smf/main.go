package main

import (
	"errors"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	smfpb "github.com/5g-core/proto/smf"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

const gracefulShutdownTimeout = 5 * time.Second

type grpcStopper interface {
	GracefulStop()
	Stop()
}

// stopGRPCServer 给在途请求一个有限的收尾窗口，超过上限后强制停止，避免
// Ctrl+C 因某个迟迟不结束的 RPC 永久卡住。
func stopGRPCServer(server grpcStopper, timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		server.Stop()
		return false
	}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err == nil && value > 0 {
		return value
	}
	return fallback
}

func main() {
	listenAddress := env("LISTEN_ADDRESS", ":50052")
	handler, err := NewSMFHandler(env("UPF_ADDRESS", "localhost:50053"), 3*time.Second, envInt("IP_POOL_SIZE", 254))
	if err != nil {
		log.Fatalf("初始化 SMF: %v", err)
	}
	defer handler.Close()
	lis, err := net.Listen("tcp", listenAddress)
	if err != nil {
		log.Fatalf("SMF 监听失败: %v", err)
	}
	server := grpc.NewServer()
	smfpb.RegisterSMFServiceServer(server, handler)
	hs := health.NewServer()
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(server, hs)
	serveErr := make(chan error, 1)
	go func() {
		log.Printf("SMF 服务启动 listen=%s upf=%s", listenAddress, env("UPF_ADDRESS", "localhost:50053"))
		serveErr <- server.Serve(lis)
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-stop:
		log.Printf("收到退出信号 signal=%s", sig)
	case err := <-serveErr:
		if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Fatalf("SMF 服务异常: %v", err)
		}
		return
	}
	signal.Stop(stop)
	hs.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	if !stopGRPCServer(server, gracefulShutdownTimeout) {
		log.Printf("SMF 优雅退出超过 %s，已强制停止", gracefulShutdownTimeout)
	}
}
