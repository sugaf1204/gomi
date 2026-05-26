#!/usr/bin/env bash

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing command: $1" >&2
    exit 1
  fi
}

detect_remote_goarch() {
  local user_name="$1"
  local host="$2"
  local remote_uname

  echo "[orchestrator] detect remote architecture" >&2
  remote_uname=$(ssh "$user_name@$host" "uname -m")
  case "$remote_uname" in
    x86_64|amd64)
      echo "amd64"
      ;;
    *)
      echo "unsupported remote arch: $remote_uname (this script currently supports x86_64 only)" >&2
      exit 1
      ;;
  esac
}

build_gomi_binary() {
  local root_dir="$1"
  local target_goarch="$2"
  local local_bin="$3"

  echo "[orchestrator] build gomi binary for linux/$target_goarch"
  (
    cd "$root_dir"
    CGO_ENABLED=0 GOOS=linux GOARCH="$target_goarch" GOCACHE=/tmp/gomi-go-build-cache go build -o "$local_bin" ./cmd/gomi
  )
}

prepare_remote_lab_dir() {
  local user_name="$1"
  local host="$2"
  local remote_dir="$3"

  echo "[orchestrator] prepare remote dir: $user_name@$host:$remote_dir"
  ssh "$user_name@$host" "mkdir -p '$remote_dir/remote'"
}

upload_remote_lab_artifacts() {
  local user_name="$1"
  local host="$2"
  local remote_dir="$3"
  local local_bin="$4"
  local remote_src_dir="$5"
  local remote_files=(
    "$remote_src_dir/run_lab.sh"
    "$remote_src_dir/cleanup.sh"
    "$remote_src_dir/setup.sh"
    "$remote_src_dir/execute.sh"
  )

  echo "[orchestrator] upload artifacts"
  scp "${remote_files[@]}" "$user_name@$host:$remote_dir/remote/"
  scp "$local_bin" "$user_name@$host:$remote_dir/gomi"
}

execute_remote_lab() {
  local user_name="$1"
  local host="$2"
  local remote_dir="$3"

  echo "[orchestrator] execute remote lab"
  ssh "$user_name@$host" "chmod +x '$remote_dir/remote/'*.sh && '$remote_dir/remote/run_lab.sh' '$remote_dir' '$remote_dir/gomi'"
}
