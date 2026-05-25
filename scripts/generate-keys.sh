#!/usr/bin/env bash
# Generate the SSH keypair used by the Ansible control node to log into
# every redis-node container. Idempotent - does nothing if the key exists.
#
# Public half:  infra/keys/redis_tool_id_ed25519.pub  (baked into the image)
# Private half: $HOME/.ssh/redis_tool_id_ed25519       (used by Ansible)
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KEY_DIR="${REDIS_TOOL_KEY_DIR:-$HOME/.ssh}"
KEY_PATH="${KEY_DIR}/redis_tool_id_ed25519"
PUB_DEST="${REPO_ROOT}/infra/keys/redis_tool_id_ed25519.pub"

mkdir -p "${KEY_DIR}"
chmod 700 "${KEY_DIR}"

if [[ ! -f "${KEY_PATH}" ]]; then
  ssh-keygen -t ed25519 -N "" -C "redis-tool@$(hostname)" -f "${KEY_PATH}" >/dev/null
  echo "Generated ${KEY_PATH}"
else
  echo "Reusing existing ${KEY_PATH}"
fi

install -m 0644 "${KEY_PATH}.pub" "${PUB_DEST}"
echo "Public key copied to ${PUB_DEST}"
