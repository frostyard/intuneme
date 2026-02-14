#!/bin/sh
set -e
mkdir -p manpages
go run . man > "manpages/intuneme.1"
gzip -f "manpages/intuneme.1"
