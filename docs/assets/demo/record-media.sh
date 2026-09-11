#!/usr/bin/env bash
# Record the animated README media from real command output.
#
# Every frame is a real terminal capture of the binary built from this
# repository, running against the demo repository that
# build-demo-repo.sh creates. Nothing is mocked or retouched: the colors
# come from git byline blame itself.
#
# Requirements: go, vhs, ttyd, ffmpeg, jq.
# Usage: record-media.sh [work-directory]
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
work="${1:-${TMPDIR:-/tmp}/git-byline-media}"
assets="$repo_root/docs/assets"

for tool in go vhs ttyd ffmpeg jq; do
  command -v "$tool" >/dev/null || { echo "missing required tool: $tool" >&2; exit 1; }
done

rm -rf "$work"
mkdir -p "$work"
work="$(cd "$work" && pwd)"

echo "building binary"
(cd "$repo_root" && CGO_ENABLED=0 go build -trimpath -o "$work/git-byline" ./cmd/git-byline)

echo "building demo repository"
"$assets/demo/build-demo-repo.sh" "$work/git-byline" "$work/demo" >/dev/null

# Shared look. The frame is Width-80 by Height-64 pixels, because vhs
# reserves an outer margin, so the sizes below are chosen to leave no
# empty band under the last output row.
# Terminal theme built from the palette in docs/DESIGN.md. Slots the
# palette does not define, such as the ANSI green and yellow positions,
# take the nearest available step rather than a new color.
terminal_theme='{ "background": "#000000", "foreground": "#FFFFFF", "cursor": "#00FFFF", "selection": "#333333", "black": "#1A1A1A", "red": "#FF4040", "green": "#00AAAA", "yellow": "#FF80CD", "blue": "#8080FF", "magenta": "#FF009B", "cyan": "#00FFFF", "white": "#BFBFBF", "brightBlack": "#A6A6A6", "brightRed": "#FF8080", "brightGreen": "#00D0D0", "brightYellow": "#FFBFE6", "brightBlue": "#BFBFFF", "brightMagenta": "#B280DF", "brightCyan": "#80FFFF", "brightWhite": "#FFFFFF" }'

# tape_header <name> <width> <height> [font-size]
#
# Width and Height are the vhs canvas; the captured frame is Width-50 by
# Height-38 pixels. Font size changes how many rows and columns fit inside
# that fixed frame, which is how the tour zooms without resizing the GIF.
tape_header() {
  cat <<EOF
Output "$work/frames-$1/"
Set Shell "bash"
Set FontSize ${4:-15}
Set Padding 10
Set Theme $terminal_theme
Set Width $2
Set Height $3
Set TypingSpeed 35ms
Hide
Type "export COLORTERM=truecolor PATH=$work:\$PATH; cd $work/demo; clear"
Enter
Sleep 800ms
Show
EOF
}

# render <name> <fps> assembles one GIF from the captured frames.
render() {
  local name="$1" fps="$2"
  echo "encoding $name"
  # frame-text holds the rendered terminal; frame-cursor is only the
  # transparent cursor layer vhs composites on top.
  ffmpeg -loglevel error -y \
    -framerate 50 -pattern_type glob -i "$work/frames-$name/frame-text-*.png" \
    -filter_complex "fps=$fps,split[a][b];[a]palettegen=max_colors=128:stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=4:diff_mode=rectangle" \
    -loop 0 "$assets/git-byline-$name.gif"
}

# tour_canvas is the hero animation size. A zoom segment rounds to whole
# character cells and can come out a few pixels smaller, so every segment
# is padded up to this exact canvas instead of being rescaled. Rescaling
# would resample the glyphs and defeat the point of the zoom.
tour_width=1250
tour_height=486

# render_sequence <output-name> <fps> <segment>...
#
# Concatenates several recordings into one animation. Each segment is a
# separate ffmpeg input, because the image demuxer stops at the first frame
# size change and the zoom segments differ by a few pixels.
render_sequence() {
  local name="$1" fps="$2"
  shift 2
  local inputs=() filter="" labels="" index=0 segment
  for segment in "$@"; do
    inputs+=(-framerate 50 -start_number 1 -i "$work/frames-$segment/frame-text-%05d.png")
    filter+="[${index}:v]fps=${fps},pad=${tour_width}:${tour_height}:0:0:color=black,setsar=1[v${index}];"
    labels+="[v${index}]"
    index=$((index + 1))
  done
  filter+="${labels}concat=n=${index}:v=1:a=0[cat];"
  filter+="[cat]split[a][b];[a]palettegen=max_colors=96:stats_mode=diff[p];"
  filter+="[b][p]paletteuse=dither=bayer:bayer_scale=4:diff_mode=rectangle"
  echo "encoding $name from $index segments"
  ffmpeg -loglevel error -y "${inputs[@]}" -filter_complex "$filter" \
    -loop 0 "$assets/git-byline-$name.gif"
}

# 1. The tour: the hero animation. Four views on one 1250 by 486 canvas,
#    which is 27 rows at font 15 and 18 rows at font 22. Every view is
#    composed to fill its rows, so none leaves an empty band, and the two
#    views at font 22 read as a zoom on the same terminal rather than a
#    resized image.
{
  tape_header tour-1 1300 524
  cat <<'EOF'
Type "git byline blame src/pricing.go"
Enter
Sleep 5200ms
EOF
} >"$work/tour-1.tape"

{
  tape_header tour-2 1300 524 22
  cat <<'EOF'
Type "git byline blame src/tax.go"
Enter
Sleep 2600ms
Type "git byline stats HEAD~6..HEAD | sed -n '/^Authors:/,/^Sessions:/p' | head -3"
Enter
Sleep 2600ms
Type "git byline disclosure --output d.json >/dev/null && du -h d.json"
Enter
Sleep 3000ms
EOF
} >"$work/tour-2.tape"

{
  tape_header tour-3 1300 524
  cat <<'EOF'
Type "git byline stats HEAD~2..HEAD"
Enter
Sleep 4200ms
Type "git byline verify HEAD~6..HEAD --deep"
Enter
Sleep 3000ms
EOF
} >"$work/tour-3.tape"

{
  tape_header tour-4 1300 524 22
  cat <<'EOF'
Type "git byline check HEAD~6..HEAD --max-ai-percent 30; echo exit=$?"
Enter
Sleep 3000ms
Type "git byline check HEAD~6..HEAD --max-ai-percent 60; echo exit=$?"
Enter
Sleep 2600ms
Type "git byline export --format gitai --output a.txt >/dev/null && head -3 a.txt"
Enter
Sleep 3200ms
EOF
} >"$work/tour-4.tape"

# 2. Range aggregation, switching from the report to its JSON contract.
{
  tape_header stats 1300 666
  cat <<'EOF'
Type "git byline stats HEAD~6..HEAD"
Enter
Sleep 5000ms
Type "clear"
Enter
Sleep 300ms
Type "git byline stats --json HEAD~6..HEAD | jq '{totals, authors}'"
Enter
Sleep 4500ms
EOF
} >"$work/stats.tape"

# 3. The policy gate and the audit pair, one exit code per view.
{
  tape_header audit 1300 200
  cat <<'EOF'
Type "git byline check HEAD~6..HEAD --max-ai-percent 30; echo exit=$?"
Enter
Sleep 4000ms
Type "clear"
Enter
Sleep 300ms
Type "git byline check HEAD~6..HEAD --max-ai-percent 60; echo exit=$?"
Enter
Sleep 3500ms
Type "clear"
Enter
Sleep 300ms
Type "git byline verify HEAD~6..HEAD --deep"
Enter
Sleep 1500ms
Type "git byline disclosure --range HEAD~6..HEAD --output disclosure.json >/dev/null"
Enter
Sleep 1200ms
Type "jq -c '{ai_share: .totals.ai_share_percent, lines: .totals.lines, agents: [.agents[].agent]}' disclosure.json"
Enter
Sleep 4000ms
EOF
} >"$work/audit.tape"

for name in tour-1 tour-2 tour-3 tour-4; do
  echo "recording $name"
  (cd "$work" && vhs "$work/$name.tape" >/dev/null)
done
render_sequence tour 10 tour-1 tour-2 tour-3 tour-4

for name in stats audit; do
  echo "recording $name"
  (cd "$work" && vhs "$work/$name.tape" >/dev/null)
  render "$name" 12
done

echo "writing dashboards"
(cd "$work/demo" && "$work/git-byline" dashboard --output "$work/dashboard-commit.html" >/dev/null)
(cd "$work/demo" && "$work/git-byline" dashboard --range HEAD~6..HEAD --output "$work/dashboard-range.html" >/dev/null)

# The dashboard is HTML, so its frames come from a headless browser at a
# 1280 by 820 viewport: page top and bottom of the commit report, then the
# same two views of the range report, saved as dash-1.png to dash-4.png.
# The encoder below is the only step this script owns.
if [ -f "$work/dash-1.png" ]; then
  echo "encoding dashboard"
  ffmpeg -loglevel error -y -framerate 0.4 -i "$work/dash-%d.png" \
    -filter_complex "fps=0.4,split[a][b];[a]palettegen=max_colors=192:stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=5:diff_mode=rectangle" \
    -loop 0 "$assets/git-byline-dashboard.gif"
fi

echo
echo "GIFs written to $assets"
echo "dashboard pages ready for capture:"
echo "  $work/dashboard-commit.html"
echo "  $work/dashboard-range.html"
