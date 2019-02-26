#!/usr/bin/env bash

# usage:
# ./run_tests.sh                         # local, go 1.12
# GOVERSION=1.11 ./run_tests.sh          # local, go 1.11
# ./run_tests.sh docker                  # docker, go 1.12
# GOVERSION=1.11 ./run_tests.sh docker   # docker, go 1.11

set -ex

[[ ! "$GOVERSION" ]] && GOVERSION=1.12
REPO=dcrwallet

# To run on docker on windows, symlink /mnt/c to /c and then execute the script
# from the repo path under /c.  See:
# https://github.com/Microsoft/BashOnWindows/issues/1854
# for more details.

testrepo () {
    go version
    env GORACE='halt_on_error=1' CC=gcc GOTESTFLAGS='-race -short' bash ./testmodules.sh
}

DOCKER=
[[ "$1" == "docker" ]] && DOCKER=docker
[[ "$1" == "podman" ]] && DOCKER=podman
if [[ ! "$DOCKER" ]]; then
    testrepo
    exit
fi

DOCKER_IMAGE_TAG=decred-golang-builder-$GOVERSION
$DOCKER pull decred/$DOCKER_IMAGE_TAG
$DOCKER run --rm -it -v $(pwd):/src:Z decred/$DOCKER_IMAGE_TAG /bin/bash -c "\
  cp -R /src ~/src && \
  cd ~/src && \
  env GOVERSION=$GOVERSION bash ./run_tests.sh"
