#!/bin/sh

docker build  -t protobuf-builder  . &&\
docker run --rm -e UID=$UID -v `pwd`:/build -it protobuf-builder

