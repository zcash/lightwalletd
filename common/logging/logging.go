package logging

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
)

var LogToStderr bool

func LoggingInterceptor() grpc.ServerOption {
	return grpc.UnaryInterceptor(LogInterceptor)
}

func loggerFromContext(ctx context.Context) *logrus.Entry {
	// TODO: anonymize the addresses. cryptopan?
	if peerInfo, ok := peer.FromContext(ctx); ok {
		return log.WithFields(logrus.Fields{"peer_addr": peerInfo.Addr})
	}
	return log.WithFields(logrus.Fields{"peer_addr": "unknown"})
}

func LogInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	reqLog := loggerFromContext(ctx)
	start := time.Now()

	resp, err := handler(ctx, req)

	if LogToStderr {
		entry := reqLog.WithFields(logrus.Fields{
			"method":   info.FullMethod,
			"duration": time.Since(start),
			"error":    err,
		})

		if err != nil {
			entry.Error("call failed")
		} else {
			entry.Info("method called")
		}
	}

	return resp, err
}
