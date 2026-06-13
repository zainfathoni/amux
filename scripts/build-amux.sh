#!/bin/sh
set -eu

output=${1:-amux}

short_sha() {
	if [ -n "${GITHUB_SHA:-}" ]; then
		printf '%s' "$GITHUB_SHA" | cut -c1-12
		return
	fi
	git rev-parse --short=12 HEAD 2>/dev/null || printf 'unknown'
}

utc_from_epoch() {
	epoch=$1
	if date -u -d "@$epoch" '+%Y-%m-%dT%H:%M:%SZ' >/dev/null 2>&1; then
		date -u -d "@$epoch" '+%Y-%m-%dT%H:%M:%SZ'
		return
	fi
	date -u -r "$epoch" '+%Y-%m-%dT%H:%M:%SZ'
}

utc_now() {
	date -u '+%Y-%m-%dT%H:%M:%SZ'
}

commit=${COMMIT:-$(short_sha)}

if [ -n "${BUILT:-}" ]; then
	built=$BUILT
elif [ -n "${SOURCE_DATE_EPOCH:-}" ]; then
	built=$(utc_from_epoch "$SOURCE_DATE_EPOCH")
else
	built=$(utc_now)
fi

if [ -n "${VERSION:-}" ]; then
	version=$VERSION
elif [ "${GITHUB_REF_TYPE:-}" = "tag" ] && [ -n "${GITHUB_REF_NAME:-}" ]; then
	version=$GITHUB_REF_NAME
elif [ -n "${PR_NUMBER:-}" ] && [ -n "${GITHUB_RUN_NUMBER:-}" ]; then
	version=pr.${PR_NUMBER}.${GITHUB_RUN_NUMBER}
elif [ "${GITHUB_REF_NAME:-}" = "main" ] && [ -n "${GITHUB_RUN_NUMBER:-}" ]; then
	version=main.${GITHUB_RUN_NUMBER}
else
	version=dev.${commit}
fi

go build \
	-ldflags "-X main.version=$version -X main.commit=$commit -X main.built=$built" \
	-o "$output" \
	./cmd/amux
