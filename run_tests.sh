#!/usr/bin/env bash
set -ex

# The script does automatic checking on a Go package and its sub-packages,
# including:
# 1. gofmt         (http://golang.org/cmd/gofmt/) (Temporarily disabled)
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
GOVERSION=${1:-1.10}
REPO=dcrwallet
DOCKER_IMAGE_TAG=decred-golang-builder-$GOVERSION

testrepo () {
  TMPFILE=$(mktemp)

  dep ensure

  # Check linters
  gometalinter --vendor --disable-all --deadline=10m -s testdata \
    \ #--enable=gofmt \
    --enable=vet \
    --enable=gosimple \
    --enable=unconvert \
    --enable=ineffassign \
    ./...
  if [ $? != 0 ]; then
    echo 'gometalinter has some complaints'
    exit 1
  fi

  # Test application install
  if [ $GOVERSION == 1.10 ]; then
    go install -i . ./cmd/...
  else
    go install . ./cmd/...
  fi
  if [ $? != 0 ]; then
    echo 'go install failed'
    exit 1
  fi

  # Check tests
  env GORACE='halt_on_error=1' go test -short -race ./...
  if [ $? != 0 ]; then
    echo 'go tests failed'
    exit 1
  fi

  echo "------------------------------------------"
  echo "Tests completed successfully!"
}

if [ $GOVERSION == "local" ]; then
    testrepo
    exit
fi

docker pull decred/$DOCKER_IMAGE_TAG
if [ $? != 0 ]; then
        echo 'docker pull failed'
        exit 1
fi

docker run --rm -it -v $(pwd):/src decred/$DOCKER_IMAGE_TAG /bin/bash -c "\
  rsync -ra --filter=':- .gitignore'  \
  /src/ /go/src/github.com/decred/$REPO/ && \
  cd github.com/decred/$REPO/ && \
  bash run_tests.sh local"
if [ $? != 0 ]; then
        echo 'docker run failed'
        exit 1
fi
