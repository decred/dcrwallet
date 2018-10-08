#!/bin/sh

set -e

GO=go
if [[ $GOVERSION == 1.10 ]]; then
    GO=vgo
fi

ROOTPATH=$($GO list -m -f {{.Dir}} 2>/dev/null)
ROOTPATHPATTERN=$(echo $ROOTPATH | sed 's/\\/\\\\/g' | sed 's/\//\\\//g')
MODPATHS=$($GO list -m -f {{.Dir}} all 2>/dev/null | grep "^$ROOTPATHPATTERN" | sed -e "s/^$ROOTPATHPATTERN//" -e 's/^\\//' -e 's/^\///')
MODPATHS=". $MODPATHS"

tests () {
    $GO test $GOTESTFLAGS ./...
}

for m in $MODPATHS; do
    (cd "$m" && tests)
done
