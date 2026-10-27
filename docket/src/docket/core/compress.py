"""Reduce file size. Runs in a helper process because MuPDF holds the GIL."""

from __future__ import annotations

import multiprocessing as mp
import os

PRESETS = {
    "lossless": None,
    "balanced": (200, 150, 75),
    "smallest": (120, 96, 60),
}


def compress_bytes(data: bytes, preset: str) -> bytes:
    import pymupdf

    doc = pymupdf.open(stream=data, filetype="pdf")
    try:
        p = PRESETS[preset]
        if p:
            threshold, target, quality = p
            doc.rewrite_images(dpi_threshold=threshold, dpi_target=target, quality=quality,
                               lossy=True, lossless=True, bitonal=False)
        try:
            doc.subset_fonts()
        except Exception:
            pass
        doc.scrub(attached_files=False, clean_pages=True, embedded_files=False, hidden_text=False,
                  javascript=False, metadata=False, redactions=False, redact_images=0,
                  remove_links=False, reset_fields=False, reset_responses=False,
                  thumbnails=True, xml_metadata=False)
        return doc.tobytes(garbage=4, deflate=True, deflate_images=True, deflate_fonts=True,
                           clean=True, use_objstms=1, compression_effort=0)
    finally:
        doc.close()


def _child(data: bytes, preset: str, out_path: str, conn):
    try:
        result = compress_bytes(data, preset)
        with open(out_path, "wb") as fh:
            fh.write(result)
        conn.send(("ok", len(result)))
    except Exception as exc:  # report to the parent
        conn.send(("error", str(exc)))
    finally:
        conn.close()


def start(data: bytes, preset: str, out_path: str):
    """Start compression in a child process. Returns (process, connection)."""
    ctx = mp.get_context("spawn")
    parent, child = ctx.Pipe(duplex=False)
    proc = ctx.Process(target=_child, args=(data, preset, out_path, child), daemon=True)
    proc.start()
    child.close()
    return proc, parent


def cancel(proc, out_path: str):
    if proc.is_alive():
        proc.terminate()
        proc.join(2)
    try:
        os.unlink(out_path)
    except OSError:
        pass
