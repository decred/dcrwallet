# The image below is golang:1.24.1-alpine3.21 (linux/amd64)
# It's pulled by the digest (immutable id) to avoid supply-chain attacks.
# Maintainer Note:
#    To update to a new digest, you must first manually pull the new image:
#    `docker pull golang:<new version>`
#    Docker will print the digest of the new image after the pull has finished.
FROM golang@sha256:43c094ad24b6ac0546c62193baeb3e6e49ce14d3250845d166c77c25f64b0386

ENV PB_REPO https://github.com/protocolbuffers/protobuf
ENV PB_VERSION 30.2

RUN wget $PB_REPO/releases/download/v$PB_VERSION/protoc-$PB_VERSION-linux-x86_64.zip
RUN unzip protoc-$PB_VERSION-linux-x86_64.zip -d $HOME/protobuf
RUN mv $HOME/protobuf/bin/protoc /usr/local/bin

WORKDIR /build/rpc
CMD ["./regen.sh"]
