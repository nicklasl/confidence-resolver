package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	pb "github.com/spotify/confidence-resolver-rust/mock-support-server/genproto/mock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type config struct {
	HTTPPort          int
	GRPCPort          int
	AccountID         string
	ResolverStatePath string
	SignedStateUri    string
	RequestLogging    bool
}

func readEnv() config {
	cfg := config{
		HTTPPort:          getenvInt("PORT_HTTP", 8081),
		GRPCPort:          getenvInt("PORT_GRPC", 9091),
		AccountID:         getenv("ACCOUNT_ID", "confidence-test"),
		ResolverStatePath: getenv("RESOLVER_STATE_PB", ""),
		SignedStateUri:    getenv("SIGNED_STATE_URI", fmt.Sprintf("http://localhost:%d/state", getenvInt("PORT_HTTP", 8081))),
		RequestLogging:    getenvBool("REQUEST_LOGGING", false),
	}
	return cfg
}

type authService struct {
	pb.UnimplementedAuthServiceServer
	accountID string
}

func (s *authService) RequestAccessToken(ctx context.Context, req *pb.RequestAccessTokenRequest) (*pb.AccessToken, error) {
	type tokenClaims struct {
		jwt.RegisteredClaims
		AccountName string `json:"https://confidence.dev/account_name"`
	}

	expiresIn := time.Hour

	claims := tokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "mock-support-server",
			Subject:   "mock-client",
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiresIn)),
		},
		AccountName: s.accountID,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte("jwt-secret"))
	if err != nil {
		return nil, fmt.Errorf("failed to sign jwt: %w", err)
	}
	return &pb.AccessToken{AccessToken: signed, ExpiresIn: int64(expiresIn.Seconds())}, nil
}

type resolverStateService struct {
	pb.UnimplementedResolverStateServiceServer
	signedUri string
	accountId string
}

func (s *resolverStateService) ResolverStateUri(ctx context.Context, req *pb.ResolverStateUriRequest) (*pb.ResolverStateUriResponse, error) {
	expireTime := timestamppb.New(time.Now().Add(24 * time.Hour))
	return &pb.ResolverStateUriResponse{SignedUri: s.signedUri, ExpireTime: expireTime, Account: s.accountId}, nil
}

type internalFlagLoggerService struct {
	pb.UnimplementedInternalFlagLoggerServiceServer
	bytesIn      atomic.Int64
	appliedCount atomic.Int64
	requestCount atomic.Int64
}

func (s *internalFlagLoggerService) WriteFlagLogs(ctx context.Context, req *pb.WriteFlagLogsRequest) (*pb.WriteFlagLogsResponse, error) {
	s.bytesIn.Add(int64(proto.Size(req)))
	s.appliedCount.Add(int64(len(req.FlagAssigned)))
	s.requestCount.Add(1)
	return &pb.WriteFlagLogsResponse{}, nil
}

func main() {
	cfg := readEnv()
	var grpcServer *grpc.Server
	if cfg.RequestLogging {
		grpcServer = grpc.NewServer(
			grpc.ChainUnaryInterceptor(unaryLoggingInterceptor),
		)
	} else {
		grpcServer = grpc.NewServer()
	}

	// Shared implementation for both gRPC and HTTP (grpc-gateway)
	iamImpl := &authService{accountID: cfg.AccountID}
	pb.RegisterAuthServiceServer(grpcServer, iamImpl)

	resolverStateImpl := &resolverStateService{signedUri: cfg.SignedStateUri, accountId: cfg.AccountID}
	pb.RegisterResolverStateServiceServer(grpcServer, resolverStateImpl)

	internalFlagLoggerServiceImpl := &internalFlagLoggerService{}
	pb.RegisterInternalFlagLoggerServiceServer(grpcServer, internalFlagLoggerServiceImpl)

	// Periodic metrics log (once per second) for the lifetime of the server
	go func() {
		ticker := time.NewTicker(time.Second)
		for range ticker.C {
			b := internalFlagLoggerServiceImpl.bytesIn.Load()
			a := internalFlagLoggerServiceImpl.appliedCount.Load()
			r := internalFlagLoggerServiceImpl.requestCount.Load()
			log.Printf("metrics bytes_total=%d applied_total=%d req_total=%d", b, a, r)
		}
	}()

	// Start HTTP grpc-gateway on a separate port
	go func() {
		ctx := context.Background()
		gw := runtime.NewServeMux(
			// Accept protobuf payloads for endpoints like /v1/flagLogs:write
			runtime.WithMarshalerOption("application/x-protobuf", &runtime.ProtoMarshaller{}),
		)
		if err := pb.RegisterAuthServiceHandlerServer(ctx, gw, iamImpl); err != nil {
			log.Fatalf("failed to register grpc-gateway handlers: %v", err)
		}
		if err := pb.RegisterResolverStateServiceHandlerServer(ctx, gw, resolverStateImpl); err != nil {
			log.Fatalf("failed to register grpc-gateway handlers: %v", err)
		}
		if err := pb.RegisterInternalFlagLoggerServiceHandlerServer(ctx, gw, internalFlagLoggerServiceImpl); err != nil {
			log.Fatalf("failed to register grpc-gateway handlers: %v", err)
		}

		// REST-only mux
		rest := http.NewServeMux()
		rest.HandleFunc("/state", func(w http.ResponseWriter, r *http.Request) {
			if cfg.ResolverStatePath == "" {
				http.Error(w, "resolver state not configured", http.StatusNotFound)
				return
			}
			f, err := os.Open(cfg.ResolverStatePath)
			if err != nil {
				log.Printf("/state open error: %v", err)
				http.Error(w, "failed to read state", http.StatusInternalServerError)
				return
			}
			defer f.Close()
			info, err := f.Stat()
			if err != nil {
				log.Printf("/state stat error: %v", err)
				http.Error(w, "failed to read state", http.StatusInternalServerError)
				return
			}
			etag := fmt.Sprintf("\"%x-%x\"", info.ModTime().Unix(), info.Size())
			if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", etag)
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
			if _, err := io.Copy(w, f); err != nil {
				log.Printf("/state write error: %v", err)
			}
		})

		// Optionally wrap REST mux with logging
		restHandler := http.Handler(rest)
		if cfg.RequestLogging {
			restHandler = withHTTPLogging(rest)
		}

		// Root mux: gateway at /, REST endpoints mounted explicitly
		root := http.NewServeMux()
		root.Handle("/", gw)
		root.Handle("/state", restHandler)
		httpAddr := fmt.Sprintf(":%d", cfg.HTTPPort)
		log.Printf("HTTP (grpc-gateway) listening on %s", httpAddr)
		if err := http.ListenAndServe(httpAddr, root); err != nil {
			log.Fatalf("http serve error: %v", err)
		}
	}()

	// Start gRPC server
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.GRPCPort))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	log.Printf("gRPC listening on :%d", cfg.GRPCPort)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("gRPC serve error: %v", err)
	}

}

// withHTTPLogging wraps an http.Handler to log method, path, status, size and duration.
func withHTTPLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		log.Printf("http %s %s status=%d size=%d dur=%s", r.Method, r.URL.RequestURI(), rec.status, rec.size, time.Since(start))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	size   int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.size += n
	return n, err
}

// gRPC server interceptors for rudimentary request logging.
func unaryLoggingInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	start := time.Now()
	resp, err := handler(ctx, req)
	st, _ := status.FromError(err)
	log.Printf("grpc unary %s code=%s dur=%s", info.FullMethod, st.Code(), time.Since(start))
	return resp, err
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getenvBool(key string, def bool) bool {
	if v := strings.ToLower(strings.TrimSpace(os.Getenv(key))); v != "" {
		switch v {
		case "1", "true", "yes", "y", "on":
			return true
		case "0", "false", "no", "n", "off":
			return false
		}
	}
	return def
}
