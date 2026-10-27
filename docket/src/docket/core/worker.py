"""Render/search worker process.

MuPDF holds Python's GIL for the whole of a page render, so rendering in a thread
would still freeze the window on heavy pages. Each worker is a separate process
with its own copy of the document bytes, fed requests one at a time over a pipe.
"""

from __future__ import annotations

from collections import deque


def main(conn):  # runs in the child process
    import pymupdf

    from . import text as textmod

    pymupdf.TOOLS.mupdf_display_errors(False)
    docs: dict = {}
    pending: deque = deque()

    def get(key, version):
        entry = docs.get(key)
        if entry and entry[0] == version:
            return entry[1]
        return None

    def recv():
        if pending:
            return pending.popleft()
        return conn.recv()

    while True:
        try:
            msg = recv()
        except (EOFError, OSError):
            return
        kind = msg[0]
        try:
            if kind == "load":
                _, key, version, data = msg
                old = docs.pop(key, None)
                if old:
                    old[1].close()
                docs[key] = (version, pymupdf.open(stream=data, filetype="pdf"))
            elif kind == "close":
                old = docs.pop(msg[1], None)
                if old:
                    old[1].close()
            elif kind == "quit":
                return
            elif kind in ("tile", "thumb"):
                _, req, key, version, page, zoom, clip = msg
                doc = get(key, version)
                if doc is None or page >= doc.page_count:
                    conn.send(("fail", req))
                    continue
                p = doc[page]
                mat = pymupdf.Matrix(zoom, zoom)
                if clip is not None:
                    # clip is in device pixels of the rotated page
                    x, y, w, h = clip
                    r = pymupdf.Rect(x / zoom, y / zoom, (x + w) / zoom, (y + h) / zoom)
                    pix = p.get_pixmap(matrix=mat, clip=r, alpha=False, annots=True)
                else:
                    pix = p.get_pixmap(matrix=mat, alpha=False, annots=True)
                conn.send(("img", req, pix.width, pix.height, pix.stride, bytes(pix.samples)))
            elif kind == "search":
                _, req, key, version, query, match_case, start = msg
                doc = get(key, version)
                if doc is None:
                    conn.send(("search_done", req, False))
                    continue
                pattern = textmod.compile_query(query, match_case)
                n = doc.page_count
                cancelled = False
                for k in range(n):
                    i = (start + k) % n
                    pt = textmod.extract(doc[i])
                    hits = [pt.rects_for(s, e) for s, e in textmod.find(pt, pattern)]
                    if hits:
                        conn.send(("hits", req, i, hits))
                    if k % 4 == 3:
                        conn.send(("search_progress", req, k + 1, n))
                    while conn.poll():
                        nxt = conn.recv()
                        if nxt[0] == "cancel" and nxt[1] == req:
                            cancelled = True
                        elif nxt[0] == "cancel":
                            pass
                        else:
                            pending.append(nxt)
                    if cancelled:
                        break
                conn.send(("search_done", req, not cancelled))
            elif kind == "cancel":
                pass
        except Exception as exc:
            req = msg[1] if len(msg) > 1 and isinstance(msg[1], int) else -1
            try:
                conn.send(("error", req, f"{type(exc).__name__}: {exc}"))
            except Exception:
                return

