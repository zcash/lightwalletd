// Copyright (c) 2019-2020 The Zcash developers
// Distributed under the MIT software license, see the accompanying
// file COPYING or https://www.opensource.org/licenses/mit-license.php .
package walletrpc

//go:generate protoc -I .  --go_out=. --go_opt=paths=source_relative ./compact_formats.proto
//go:generate protoc -I .  --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative ./service.proto
//go:generate protoc -I .  --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative ./darkside.proto
