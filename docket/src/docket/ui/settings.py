"""Persistent settings. Registry on Windows (HKCU\\Software\\804 Industries\\Docket),
or an ini file when DOCKET_SETTINGS_DIR is set (tests)."""

from __future__ import annotations

import os

from PySide6.QtCore import QSettings

from .. import APP_ID, ORG_NAME

MAX_RECENT = 15


def settings() -> QSettings:
    d = os.environ.get("DOCKET_SETTINGS_DIR")
    if d:
        os.makedirs(d, exist_ok=True)
        return QSettings(os.path.join(d, "docket.ini"), QSettings.Format.IniFormat)
    return QSettings(ORG_NAME, APP_ID)


def get(key: str, default=None, type_=None):
    s = settings()
    if type_ is not None:
        try:
            return s.value(key, default, type=type_)
        except Exception:
            return default
    return s.value(key, default)


def put(key: str, value):
    s = settings()
    s.setValue(key, value)
    s.sync()


def recent() -> list[str]:
    v = settings().value("recent", [])
    if isinstance(v, str):
        v = [v] if v else []
    return [p for p in (v or []) if p]


def add_recent(path: str):
    path = os.path.abspath(path)
    items = [p for p in recent() if os.path.normcase(p) != os.path.normcase(path)]
    items.insert(0, path)
    put("recent", items[:MAX_RECENT])


def clear_recent():
    put("recent", [])


def author() -> str:
    name = get("author", "")
    if name:
        return name
    return os.environ.get("USERNAME") or os.environ.get("USER") or ""
