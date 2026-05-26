package pxehttp

const targetUEFIBootOrderCleanupScript = `#!/bin/sh
set -eu

[ -d /sys/firmware/efi ] || exit 0
command -v efibootmgr >/dev/null 2>&1 || exit 0

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

efibootmgr -v > "$tmpdir/before" 2>/dev/null || exit 0
boot_current=$(sed -n 's/^BootCurrent:[[:space:]]*//p' "$tmpdir/before" | sed -n '1p' | tr -d ' ' | tr '[:lower:]' '[:upper:]')
boot_next=$(sed -n 's/^BootNext:[[:space:]]*//p' "$tmpdir/before" | sed -n '1p' | tr -d ' ' | tr '[:lower:]' '[:upper:]')
boot_order=$(sed -n 's/^BootOrder:[[:space:]]*//p' "$tmpdir/before" | sed -n '1p' | tr -d ' ' | tr '[:lower:]' '[:upper:]')
[ -n "$boot_order" ] || exit 0

if [ -n "$boot_next" ]; then
	logger -t gomi-bootorder "clearing BootNext $boot_next"
	efibootmgr -N >/dev/null 2>&1 || true
fi

: > "$tmpdir/pxe4"
: > "$tmpdir/pxe6"
while IFS= read -r line; do
	case "$line" in
		Boot[0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f][0-9A-Fa-f]*)
			id=${line#Boot}
			id=${id%%[!0-9A-Fa-f]*}
			id=$(printf '%s' "$id" | tr '[:lower:]' '[:upper:]')
			printf '%s\n' "$line" | grep -Eiq 'PXE.*IPv4|IPv4\(' && printf '%s\n' "$id" >> "$tmpdir/pxe4"
			printf '%s\n' "$line" | grep -Eiq 'PXE.*IPv6|IPv6\(' && printf '%s\n' "$id" >> "$tmpdir/pxe6"
			;;
	esac
done < "$tmpdir/before"

while IFS= read -r id; do
	[ -n "$id" ] || continue
	logger -t gomi-bootorder "deleting PXE IPv6 boot entry $id"
	efibootmgr -b "$id" -B >/dev/null 2>&1 || true
done < "$tmpdir/pxe6"

new_order=""
append_order() {
	id=$(printf '%s' "$1" | tr '[:lower:]' '[:upper:]')
	[ -n "$id" ] || return 0
	case ",$new_order," in
		*,"$id",*) return 0 ;;
	esac
	if [ -z "$new_order" ]; then
		new_order="$id"
	else
		new_order="$new_order,$id"
	fi
}

for raw_id in $(printf '%s' "$boot_order" | tr ',' ' '); do
	id=$(printf '%s' "$raw_id" | tr '[:lower:]' '[:upper:]')
	grep -qi "^$id$" "$tmpdir/pxe4" && append_order "$id"
done
append_order "$boot_current"
for raw_id in $(printf '%s' "$boot_order" | tr ',' ' '); do
	id=$(printf '%s' "$raw_id" | tr '[:lower:]' '[:upper:]')
	grep -qi "^$id$" "$tmpdir/pxe4" && continue
	grep -qi "^$id$" "$tmpdir/pxe6" && continue
	[ "$id" = "$boot_current" ] && continue
	append_order "$id"
done

[ -n "$new_order" ] || exit 0
logger -t gomi-bootorder "setting BootOrder $new_order"
efibootmgr -o "$new_order" >/dev/null 2>&1 || true
`

const targetUEFIBootOrderCleanupService = `[Unit]
Description=GOMI UEFI BootOrder cleanup
After=sysinit.target
ConditionPathExists=/sys/firmware/efi

[Service]
Type=oneshot
ExecStart=/usr/local/sbin/gomi-fix-uefi-bootorder

[Install]
WantedBy=multi-user.target
`

func injectTargetUEFIBootOrderCleanup(cfg map[string]any, runCmd *[]string) {
	writeFiles := []any{}
	if existing, ok := cfg["write_files"].([]any); ok {
		writeFiles = existing
	}
	writeFiles = append(writeFiles, map[string]any{
		"path":        "/usr/local/sbin/gomi-fix-uefi-bootorder",
		"permissions": "0755",
		"content":     targetUEFIBootOrderCleanupScript,
	}, map[string]any{
		"path":        "/etc/systemd/system/gomi-bootorder-cleanup.service",
		"permissions": "0644",
		"content":     targetUEFIBootOrderCleanupService,
	})
	cfg["write_files"] = writeFiles
	*runCmd = append(*runCmd, "systemctl enable --now gomi-bootorder-cleanup.service || /usr/local/sbin/gomi-fix-uefi-bootorder || true")
}
