"""Saved signature images, encrypted with Windows DPAPI so only the current
Windows account on this PC can read them. On other systems (development only)
they are stored unencrypted."""

from __future__ import annotations

import ctypes
import io
import os
import sys
import time

from ..paths import data_dir


def folder() -> str:
    d = os.path.join(data_dir(), "signatures")
    os.makedirs(d, exist_ok=True)
    return d


if sys.platform == "win32":
    from ctypes import wintypes

    class _BLOB(ctypes.Structure):
        _fields_ = [("cbData", wintypes.DWORD), ("pbData", ctypes.POINTER(ctypes.c_char))]

    _crypt32 = ctypes.windll.crypt32
    _kernel32 = ctypes.windll.kernel32

    def _blob(data: bytes) -> _BLOB:
        buf = ctypes.create_string_buffer(data, len(data))
        return _BLOB(len(data), ctypes.cast(buf, ctypes.POINTER(ctypes.c_char)))

    def _protect(data: bytes) -> bytes:
        src, out = _blob(data), _BLOB()
        if not _crypt32.CryptProtectData(ctypes.byref(src), "Docket signature", None, None, None,
                                         0x1, ctypes.byref(out)):
            raise ctypes.WinError()
        try:
            return ctypes.string_at(out.pbData, out.cbData)
        finally:
            _kernel32.LocalFree(out.pbData)

    def _unprotect(data: bytes) -> bytes:
        src, out = _blob(data), _BLOB()
        if not _crypt32.CryptUnprotectData(ctypes.byref(src), None, None, None, None, 0x1,
                                           ctypes.byref(out)):
            raise ctypes.WinError()
        try:
            return ctypes.string_at(out.pbData, out.cbData)
        finally:
            _kernel32.LocalFree(out.pbData)

    EXT = ".sig"
else:
    def _protect(data: bytes) -> bytes:
        return data

    def _unprotect(data: bytes) -> bytes:
        return data

    EXT = ".png"


def save(png: bytes) -> str:
    path = os.path.join(folder(), f"{int(time.time() * 1000)}{EXT}")
    with open(path, "wb") as fh:
        fh.write(_protect(png))
    return path


def load_all() -> list[tuple[str, bytes]]:
    out = []
    for name in sorted(os.listdir(folder())):
        if not name.endswith(EXT):
            continue
        path = os.path.join(folder(), name)
        try:
            with open(path, "rb") as fh:
                out.append((path, _unprotect(fh.read())))
        except Exception:
            continue  # created by another account or PC
    return out


def delete(path: str):
    try:
        os.unlink(path)
    except OSError:
        pass


def clean_scan(data: bytes, threshold: int = 200) -> bytes:
    """Turn a photo/scan of a signature into a tight PNG with a transparent
    background: near-white pixels become transparent, ink is kept."""
    from PIL import Image, ImageOps

    im = Image.open(io.BytesIO(data))
    im = ImageOps.exif_transpose(im).convert("RGBA")
    if max(im.size) > 1600:
        im.thumbnail((1600, 1600))
    gray = ImageOps.autocontrast(im.convert("L"), cutoff=1)
    w, h = gray.size
    span = max(1, threshold - 60)
    alpha = gray.point([0 if v >= threshold else min(255, (threshold - v) * 255 // span)
                        for v in range(256)])
    ink = Image.new("RGBA", (w, h), (20, 20, 60, 0))
    ink.putalpha(alpha)
    box = alpha.getbbox()
    if box:
        pad = 6
        box = (max(0, box[0] - pad), max(0, box[1] - pad), min(w, box[2] + pad), min(h, box[3] + pad))
        ink = ink.crop(box)
    buf = io.BytesIO()
    ink.save(buf, "PNG")
    return buf.getvalue()
