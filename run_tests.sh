#!/usr/bin/env bash
set -ex

# The script does automatic checking on a Go package and its sub-packages,
# including:
# 1. gofmt         (http://golang.org/cmd/gofmt/)
# 2. golint        (https://github.com/golang/lint)
# 3. go vet        (http://golang.org/cmd/vet)
# 4. gosimple      (https://github.com/dominikh/go-simple)
# 5. unconvert     (https://github.com/mdempsky/unconvert)
# 6. ineffassign   (https://github.com/gordonklaus/ineffassign)
# 7. race detector (http://blog.golang.org/race-detector)

# gometalinter (github.com/alecthomas/gometalinter) is used to run each each
# static checker.

# To run on docker on windows, symlink /mnt/c to /c and then execute the script
# from the repo path under /c.  See:
# https://github.com/Microsoft/BashOnWindows/issues/1854
# for more details.

#Default GOVERSION
GOVERSION=${1:-1.9}
REPO=dcrwallet

TESTCMD="test -z \"\$(gometalinter -j 4 --disable-all \
  --enable=gofmt \
  --enable=gosimple \
  --enable=unconvert \
  --enable=ineffassign \
  --vendor \
  --deadline=10m ./... 2>&1 | egrep -v 'testdata/' | tee /dev/stderr)\" && \
  test -z \"\$(gometalinter -j 4 --disable-all \
  --enable=golint \
  --vendor \
  --deadline=10m ./... 2>&1 | egrep -v '(ALL_CAPS|OP_|NewFieldVal|RpcCommand|RpcRawCommand|RpcSend|Dns|api.pb.go|StartConsensusRpc|factory_test.go|legacy|UnstableAPI|_string.go)' | tee /dev/stderr)\" && \
  test -z \"\$(gometalinter -j 4 --disable-all \
  --enable=vet \
  --vendor \
  --deadline=10m ./... 2>&1 | egrep -v 'not a string in call to [A-Za-z]+f' | tee /dev/stderr)\" && \
  env GORACE='halt_on_error=1' go test -short -race \$(glide novendor)"

if [ $GOVERSION == "local" ]; then
    eval $TESTCMD
    exit
fi

DOCKER_IMAGE_TAG=decred-golang-builder-$GOVERSION

docker pull decred/$DOCKER_IMAGE_TAG

docker run --rm -it -v $(pwd):/src decred/$DOCKER_IMAGE_TAG /bin/bash -c "\
  rsync -ra --filter=':- .gitignore'  \
  /src/ /go/src/github.com/decred/$REPO/ && \
  cd github.com/decred/$REPO/ && \
  glide install && \
  go install \$(glide novendor) && \
  $TESTCMD
"

echo "------------------------------------------"
echo "Tests complete."
