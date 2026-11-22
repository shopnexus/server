#!/bin/sh
# Puts the country database of the access log at <file>, checked against
# its sha256: ip-location-db's geo-whois-asn-country (CC0), IPv4 and IPv6.
#   fetch-geoip.sh <file>
# The image builds run it; GEOIP_DB names the file.
set -eu
file=${1:?usage: fetch-geoip.sh <file>}
URL=https://cdn.jsdelivr.net/npm/@ip-location-db/geo-whois-asn-country-mmdb@2.3.2026061719/geo-whois-asn-country.mmdb
SUM=c5f7c0e9bd6a6ccb5a2e9634764544a0d08bd0bd8788b8aedfac87bea184c652

mkdir -p "$(dirname "$file")"
curl -fsSL --retry 3 -o "$file" "$URL"
printf '%s  %s\n' "$SUM" "$file" | sha256sum -c -
