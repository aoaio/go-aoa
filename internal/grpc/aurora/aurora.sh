#!/usr/bin/env bash
file_name="aurora"
proto_dir="/home/he/aoa/go/src/github.com/Aurorachain/go-aoa/internal/grpc/${file_name}"
protoc -I ${proto_dir} ${proto_dir}/${file_name}.proto --go_out=plugins=grpc:${proto_dir}