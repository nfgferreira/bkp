#!/bin/bash
# Compile this way before creating Arch package
export CGO_LDFLAGS="$LDFLAGS -Wl,-z,relro,-z,now"
go build -buildmode=pie -ldflags "-linkmode=external -extldflags=$LDFLAGS" bkp.go
strip bkp
 
