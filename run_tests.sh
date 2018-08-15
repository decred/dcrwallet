#!/usr/bin/env bash

# usage:
# ./run_tests.sh                         # local, go 1.11
# GOVERSION=1.10 ./run_tests.sh          # local, go 1.10 (vgo)
# ./run_tests.sh docker                  # docker, go 1.11
# GOVERSION=1.10 ./run_tests.sh docker   # docker, go 1.10 (vgo)

set -ex

[[ ! "$GOVERSION" ]] && GOVERSION=1.11
REPO=dcrwallet

# To run on docker on windows, symlink /mnt/c to /c and then execute the script
# from the repo path under /c.  See:
# https://github.com/Microsoft/BashOnWindows/issues/1854
# for more details.

testrepo () {
    go version
    env GORACE='halt_on_error=1' CC=gcc GOTESTFLAGS='-race -short' bash ./testmodules.sh
}

if [[ "$1" != "docker" ]]; then
    testrepo
    exit
fi

DOCKER_IMAGE_TAG=decred-golang-builder-$GOVERSION
docker pull decred/$DOCKER_IMAGE_TAG
docker run --rm -it -v $(pwd):/src decred/$DOCKER_IMAGE_TAG /bin/bash -c "\
  rsync -ra --filter=':- .gitignore'  \
  /src/ /go/src/github.com/decred/$REPO/ && \
  cd github.com/decred/$REPO/ && \
  env GOVERSION=$GOVERSION GO111MODULE=on bash run_tests.sh"
