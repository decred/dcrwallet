#!/usr/bin/env bash

set -ex

go version

GOTESTFLAGS='-short'
ROOTPATH=$(go list -m -f {{.Dir}} 2>/dev/null)
ROOTPATHPATTERN=$(echo $ROOTPATH | sed 's/\\/\\\\/g' | sed 's/\//\\\//g')
MODPATHS=$(go list -m -f {{.Dir}} all 2>/dev/null | grep "^$ROOTPATHPATTERN" | sed -e "s/^$ROOTPATHPATTERN//" -e 's/^\\//' -e 's/^\///')
MODPATHS=". $MODPATHS"

for m in $MODPATHS; do
    (cd "$m" && go test $GOTESTFLAGS ./...)
done
