#!/usr/bin/env bash
# add grpc path, may use proto file in this path
grpc_path="/home/he/aoa/go/src/github.com/Aurorachain/go-aoa/internal/grpc/"
file_name="transactionpool"
proto_dir="/home/he/aoa/go/src/github.com/Aurorachain/go-aoa/internal/grpc/${file_name}"
#protoc --proto_path=${auraro_path} --proto_path=${personal_path} -I ${proto_dir} ${proto_dir}/${file_name}.proto --go_out=plugins=grpc:${proto_dir}
protoc --proto_path=${grpc_path} --proto_path=${proto_dir} --go_out=plugins=grpc:${grpc_path} ${proto_dir}/${file_name}.proto