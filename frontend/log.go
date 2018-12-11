package frontend

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/peer"
)

var log *logrus.Entry
var logger = logrus.New()

func init() {
	logger.SetFormatter(&logrus.TextFormatter{
		//DisableColors:          true,
		FullTimestamp:          true,
		DisableLevelTruncation: true,
	})

	log = logger.WithFields(logrus.Fields{
		"app": "frontend-grpc",
	})
}

func LoggingInterceptor() grpc.ServerOption {
	return grpc.UnaryInterceptor(logInterceptor)
}

func logInterceptor(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler,
) (interface{}, error) {
	reqLog := loggerFromContext(ctx)
	start := time.Now()

	resp, err := handler(ctx, req)

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

	return resp, err
}

func loggerFromContext(ctx context.Context) *logrus.Entry {
	// TODO: anonymize the addresses. cryptopan?
	if peerInfo, ok := peer.FromContext(ctx); ok {
		return log.WithFields(logrus.Fields{"peer_addr": peerInfo.Addr})
	}
	return log.WithFields(logrus.Fields{"peer_addr": "unknown"})
}
