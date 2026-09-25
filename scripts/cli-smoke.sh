#!/usr/bin/env bash
set -euo pipefail

binary=${1:-bin/vikunja-cli}

"$binary" user get | python3 -c '
import json
import sys

user = json.load(sys.stdin)
valid = (
    isinstance(user, dict)
    and type(user.get("id")) is int
    and user["id"] > 0
    and isinstance(user.get("username"), str)
    and bool(user["username"].strip())
)
if not valid:
    raise SystemExit("current-user response lacks positive id or username")
'
