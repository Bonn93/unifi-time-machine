#!/bin/sh
# Point Go at Chainguard Libraries for Go and store a short-lived token for it.
#
#   sh scripts/chainguard-go-auth.sh            # refresh the token in ~/.netrc
#   sh scripts/chainguard-go-auth.sh FILE       # write a standalone netrc to FILE
#                                               # (build.sh uses this for docker builds)
#
# Needs chainctl, logged in (`chainctl auth login`) as a member of an
# organization entitled to Chainguard Libraries for Go. The token expires
# after about an hour; rerun this script when Go reports 401 Unauthorized.
set -eu

host=libraries.cgr.dev
proxy="https://${host}/go/"

token="$(chainctl auth token --audience "${host}")"
entry="$(printf 'machine %s\nlogin _token\npassword %s\n' "${host}" "${token}")"

umask 077
if [ $# -gt 0 ]; then
	printf '%s\n' "${entry}" > "$1"
	exit 0
fi

netrc="${NETRC:-${HOME}/.netrc}"
touch "${netrc}"
chmod 600 "${netrc}"
# Replace any existing entry for the host, keeping every other machine
tmp="$(mktemp)"
awk -v host="${host}" '
	$1 == "machine" || $1 == "default" { skip = ($1 == "machine" && $2 == host) }
	!skip { print }
' "${netrc}" > "${tmp}"
printf '%s\n' "${entry}" >> "${tmp}"
cat "${tmp}" > "${netrc}"
rm -f "${tmp}"

if [ "$(go env GOPROXY 2>/dev/null)" != "${proxy}" ]; then
	go env -w GOPROXY="${proxy}"
	echo "Set GOPROXY=${proxy} (undo with: go env -u GOPROXY)"
fi
echo "Wrote a ${host} token to ${netrc}; it expires in about an hour."
