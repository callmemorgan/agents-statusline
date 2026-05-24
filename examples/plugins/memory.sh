#!/bin/bash
# Memory monitoring plugin for claude-statusline
# Outputs key:value lines for multi-field consumption.
#
# Config entry:
#   {
#     "command": "~/.config/claude-statusline/plugins/memory.sh",
#     "timeout_ms": 200,
#     "fields": [
#       {"id": "mem-used",     "line": 1, "desc": "RAM used"},
#       {"id": "swap-used",    "line": 1, "desc": "Swap used"},
#       {"id": "%-mem-used",   "line": 1, "desc": "RAM % used"}
#     ]
#   }

set -e

fmt_bytes() {
	local bytes=$1
	if [ "$bytes" -ge 1073741824 ]; then
		awk "BEGIN {printf \"%.1fG\", $bytes/1073741824}"
	elif [ "$bytes" -ge 1048576 ]; then
		awk "BEGIN {printf \"%.0fM\", $bytes/1048576}"
	else
		awk "BEGIN {printf \"%.0fK\", $bytes/1024}"
	fi
}

if [ "$(uname -s)" = "Darwin" ]; then
	page_size=$(pagesize)
	vm=$(vm_stat)

	parse_pages() {
		echo "$vm" | awk -v key="$1" '
		{
			gsub(/\./, "")
			if ($0 ~ key) {
				for (i = 1; i <= NF; i++) {
					if ($i ~ /^[0-9]+$/) {
						print $i
						exit
					}
				}
			}
		}'
	}

	free=$(parse_pages "Pages free")
	active=$(parse_pages "Pages active")
	inactive=$(parse_pages "Pages inactive")
	speculative=$(parse_pages "Pages speculative")
	wired=$(parse_pages "Pages wired down")
	compressed=$(parse_pages "Pages occupied by compressor")

	free=${free:-0}
	active=${active:-0}
	inactive=${inactive:-0}
	speculative=${speculative:-0}
	wired=${wired:-0}
	compressed=${compressed:-0}

	total=$(( (free + active + inactive + speculative + wired + compressed) * page_size ))
	used=$(( (active + inactive + wired + compressed) * page_size ))

	swap_info=$(sysctl -n vm.swapusage 2>/dev/null || true)
	if [ -n "$swap_info" ]; then
		swap_used=$(echo "$swap_info" | sed -n 's/.*used = \([0-9.]*[KMGTPEZY]\).*/\1/p')
	fi
	swap_used=${swap_used:-0M}
else
	meminfo=$(cat /proc/meminfo)

	mem_total=$(echo "$meminfo" | awk '/MemTotal/ {print $2}')
	mem_available=$(echo "$meminfo" | awk '/MemAvailable/ {print $2}')

	if [ -z "$mem_available" ]; then
		mem_free=$(echo "$meminfo" | awk '/MemFree/ {print $2}')
		buffers=$(echo "$meminfo" | awk '/^Buffers/ {print $2}')
		cached=$(echo "$meminfo" | awk '/^Cached/ {print $2}')
		mem_available=$((mem_free + buffers + cached))
	fi

	used=$(( (mem_total - mem_available) * 1024 ))
	total=$(( mem_total * 1024 ))

	swap_total=$(echo "$meminfo" | awk '/SwapTotal/ {print $2}')
	swap_free=$(echo "$meminfo" | awk '/SwapFree/ {print $2}')
	swap_used_kb=$((swap_total - swap_free))

	swap_used=$(fmt_bytes $((swap_used_kb * 1024)))
fi

pct=$(awk "BEGIN {printf \"%.0f\", ($used / $total) * 100}")
mem_used=$(fmt_bytes "$used")

echo "mem-used: ${mem_used}"
echo "swap-used: ${swap_used}"
echo "%-mem-used: ${pct}%"
