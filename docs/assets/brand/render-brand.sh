#!/bin/sh
# Render the brand images from their HTML sources with headless Chrome.
#
# Outputs:
#   docs/assets/icon.png            512x512  square mark
#   docs/assets/social-preview.png  1280x640 repository social preview
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
out_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)

chrome="${CHROME:-}"
if [ -z "$chrome" ]; then
	for candidate in \
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
		"/Applications/Chromium.app/Contents/MacOS/Chromium" \
		"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge" \
		"$(command -v google-chrome || true)" \
		"$(command -v chromium || true)"; do
		if [ -n "$candidate" ] && [ -x "$candidate" ]; then
			chrome="$candidate"
			break
		fi
	done
fi
if [ -z "$chrome" ]; then
	echo "render-brand.sh: no Chrome or Chromium found, set CHROME" >&2
	exit 1
fi

shot() {
	source_file="$script_dir/$1"
	target="$out_dir/$2"
	size="$3"
	work=$(mktemp -d)
	rm -f "$target"
	"$chrome" \
		--headless=new \
		--disable-gpu \
		--no-first-run \
		--no-default-browser-check \
		--no-pings \
		--disable-extensions \
		--disable-background-networking \
		--disable-sync \
		--disable-component-update \
		--hide-scrollbars \
		--force-device-scale-factor=1 \
		--user-data-dir="$work" \
		--virtual-time-budget=2000 \
		--window-size="$size" \
		--screenshot="$target" \
		"file://$source_file" >/dev/null 2>&1 &
	chrome_pid=$!
	# Chrome keeps running after writing the file, so stop it once it lands.
	i=0
	while [ "$i" -lt 30 ]; do
		sleep 1
		if [ -s "$target" ]; then
			sleep 1
			break
		fi
		i=$((i + 1))
	done
	kill -9 "$chrome_pid" 2>/dev/null || true
	wait "$chrome_pid" 2>/dev/null || true
	rm -rf "$work"
	if [ ! -s "$target" ]; then
		echo "render-brand.sh: $2 was not written" >&2
		exit 1
	fi
	printf 'wrote %s (%s)\n' "$target" "$size"
}

shot icon.html icon.png 512,512
shot social-preview.html social-preview.png 1280,640
