#! /bin/bash

set -e

version=$(git describe)
export GO15VENDOREXPERIMENT="1" 
rm -f ht*

echo "### Using $(go version)"

LDFLAGS="-X main.version=$version -s"

echo
echo "### Linux version"
GOOS=linux GOARCH=amd64 go build -o ht_linux -ldflags "$LDFLAGS"
# Pack binaries with goupx (github.com/pwaller/goupx) which
# uses upx (http://upx.sourceforge.net)
goupx --strip-binary ht_linux

echo
echo "### Mac OS X version"
GOOS=darwin GOARCH=amd64 go build -o ht_darwin -ldflags "$LDFLAGS"

echo
echo "### Windows version"
GOOS=windows GOARCH=amd64 go build -o ht_windows.exe -ldflags "$LDFLAGS"

echo
echo "### Check documentation"
(cd ../../ht/; ./list-checks.bash;) > CheckDocumentation.txt
ls -l CheckDocumentation.txt

source <(go env)
echo
echo "Successfully built $(./ht_$GOHOSTOS version)"

