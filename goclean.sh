#!/bin/bash
# The script does automatic checking on a Go package and its sub-packages, including:
# 1. gofmt         (http://golang.org/cmd/gofmt/)
# 2. golint        (https://github.com/golang/lint)
# 3. go vet        (http://golang.org/cmd/vet)
# 4. gosimple      (https://github.com/dominikh/go-simple)
# 5. unconvert     (https://github.com/mdempsky/unconvert)
# 6. race detector (http://blog.golang.org/race-detector)
#
# gometalinter (github.com/alecthomas/gometalinter) is used to run each static
# checker.

set -ex

# Automatic checks
test -z "$(gometalinter --disable-all --enable=gofmt --enable=gosimple --enable=unconvert --vendor --deadline=10m . 2>&1 | tee /dev/stderr)"
test -z "$(gometalinter --disable-all --enable=golint --vendor . 2>&1 | egrep -v '(ALL_CAPS|OP_|NewFieldVal|RpcCommand|RpcRawCommand|RpcSend|Dns|api.pb.go|StartConsensusRpc|factory_test.go|legacy|UnstableAPI|_string.go)' | tee /dev/stderr)"
test -z "$(gometalinter --disable-all --enable=vet --vendor . 2>&1 | egrep -v 'not a string in call to [A-Za-z]+f' | tee /dev/stderr)"

env GORACE="halt_on_error=1" go test -race -short $(glide novendor)

# Run test coverage on each subdirectories and merge the coverage profile.

#set +x
#echo "mode: count" > profile.cov

# Standard go tooling behavior is to ignore dirs with leading underscores.
#for dir in $(find . -maxdepth 10 -not -path '.' -not -path './.git*' \
#    -not -path '*/_*' -not -path './cmd*' -not -path './release*' -type d)
#do
#if ls $dir/*.go &> /dev/null; then
#  go test -covermode=count -coverprofile=$dir/profile.tmp $dir
#  if [ -f $dir/profile.tmp ]; then
#    cat $dir/profile.tmp | tail -n +2 >> profile.cov
#    rm $dir/profile.tmp
#  fi
#fi
#done

# To submit the test coverage result to coveralls.io,
# use goveralls (https://github.com/mattn/goveralls)
# goveralls -coverprofile=profile.cov -service=travis-ci
