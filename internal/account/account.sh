#!/usr/bin/env bash
# add grpc path, may use proto file in this path
grpc_path="/home/he/aoa/go/src/github.com/Aurorachain/go-aoa/internal/grpc/"
file_name="account"
proto_dir="/home/he/aoa/go/src/github.com/Aurorachain/go-aoa/internal/grpc/${file_name}"
protoc -I ${grpc_path} ${proto_dir}/${file_name}.proto --go_out=plugins=grpc:${grpc_path}
#protoc --proto_path=${grpc_path} --go_out=plugins=grpc:${grpc_path} ${proto_dir}/${file_name}.proto
