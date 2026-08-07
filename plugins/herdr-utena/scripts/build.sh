#!/bin/sh
set -eu

cd "$(dirname "$0")/.."
mkdir -p bin
go build -o bin/herdr-utena .
