"""Per-user data folders."""

from __future__ import annotations

import os
import sys

from . import APP_ID


def data_dir() -> str:
    override = os.environ.get("DOCKET_DATA_DIR")
    if override:
        base = override
    elif sys.platform == "win32":
        base = os.path.join(os.environ.get("LOCALAPPDATA") or os.path.expanduser("~"), APP_ID)
    else:
        base = os.path.join(os.environ.get("XDG_DATA_HOME") or os.path.expanduser("~/.local/share"), APP_ID)
    os.makedirs(base, exist_ok=True)
    return base


def log_dir() -> str:
    d = os.path.join(data_dir(), "logs")
    os.makedirs(d, exist_ok=True)
    return d
