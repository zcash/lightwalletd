package rpc

//go:generate protoc -I . ./compact_formats.proto --go_out=plugins=grpc:.
//go:generate protoc -I . ./service.proto --go_out=plugins=grpc:.
