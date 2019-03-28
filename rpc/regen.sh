#!/bin/sh

protoc -I. api.proto --go_out=plugins=grpc:walletrpc

# fix uid mapping on files created within the container
if [ "$UID" != "" ]; then
    chown -R $UID .  2>/dev/null
fi
