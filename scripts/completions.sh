#!/bin/sh
set -e
mkdir -p completions
for sh in bash zsh fish; do
    go run . completion "$sh" > "completions/intuneme.$sh"
done
