#!/bin/sh

generate() {
    protoc -I. api.proto --go_out=walletrpc --go-grpc_out=walletrpc \
        --go_opt=paths=source_relative --go-grpc_opt=paths=source_relative

    # fix uid mapping on files created within the container
    [ -n "$UID" ] && chown -R $UID . 2>/dev/null || return 0
}

# Add protoc-gen-go and protoc-gen-go-grpc bins to PATH before invoking protoc.
# There is an open issue to integrate go tool into protoc; if it ever gets
# implemented PATH will no longer need to manually modified here.
# https://github.com/protocolbuffers/protobuf/issues/23509
PATH=$PWD/tools/bin:$PATH generate
