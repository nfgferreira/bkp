#!/bin/bash
# Compile this way before creating Arch package
go build -buildmode=pie -ldflags "-linkmode=external -extldflags=$LDFLAGS" bkp.go
strip bkp
 
