#!/bin/sh

protoc -I. api.proto --go_out=plugins=grpc:matcherrpc
protoc -I. dicemix.proto --go_out=plugins=grpc:messages
