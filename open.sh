#!/bin/sh -eu

_open() {
    if command -v open >/dev/null 2>&1; then
        open "$@"
    else
        xdg-open "$@"
    fi
}

curl "$@" | sed -n '/^Timeline:/,/^$/{/^Timeline/n;p;}' >./events.txt
go run ./events.go <./events.txt >./events.json
./catapult/tracing/bin/trace2html ./events.json --output=./events.html
_open ./events.html
