#!/bin/sh
packr2
env GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "-s -w"
