#!/bin/bash
# Renders the OG images into ../../wwwroot (1200x630). Requires chromium.
cd "$(dirname "$0")"
for n in og og-timeline; do
  chromium-browser --headless=new --no-sandbox --hide-scrollbars --force-device-scale-factor=1 \
    --window-size=1200,630 --virtual-time-budget=4000 --screenshot="../../wwwroot/$n.png" "file://$PWD/$n.html" 2>/dev/null
  echo "rendered wwwroot/$n.png"
done
