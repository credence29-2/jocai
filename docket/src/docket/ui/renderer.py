"""Qt side of the render workers: scheduling, tile cache, restarts."""

from __future__ import annotations

import itertools
import logging
import multiprocessing as mp
import threading
import time
from collections import OrderedDict

from PySide6.QtCore import QObject, QTimer, Signal
from PySide6.QtGui import QImage

from ..core import worker

log = logging.getLogger(__name__)

CACHE_BYTES = 400 * 1024 * 1024
STUCK_AFTER = 1.5  # seconds before an unwanted slow render gets the worker restarted

_req_ids = itertools.count(1)


class ImageCache:
    def __init__(self, limit=CACHE_BYTES):
        self.limit = limit
        self.size = 0
        self._d: OrderedDict = OrderedDict()

    def get(self, key):
        img = self._d.get(key)
        if img is not None:
            self._d.move_to_end(key)
        return img

    def put(self, key, img: QImage):
        old = self._d.pop(key, None)
        if old is not None:
            self.size -= old.sizeInBytes()
        self._d[key] = img
        self.size += img.sizeInBytes()
        while self.size > self.limit and self._d:
            _, ev = self._d.popitem(last=False)
            self.size -= ev.sizeInBytes()

    def __contains__(self, key):
        return key in self._d


class Request:
    __slots__ = ("id", "kind", "doc_key", "version", "page", "zoom", "clip", "cache_key", "extra",
                 "sent_at")

    def __init__(self, kind, doc_key, version, page, zoom=1.0, clip=None, cache_key=None, extra=None):
        self.id = next(_req_ids)
        self.kind = kind
        self.doc_key = doc_key
        self.version = version
        self.page = page
        self.zoom = zoom
        self.clip = clip
        self.cache_key = cache_key
        self.extra = extra
        self.sent_at = 0.0


class RenderClient(QObject):
    """One worker process. Requests are sent one at a time so the queue can be
    re-prioritised as the user scrolls."""

    image_ready = Signal(object, object)          # cache_key, QImage
    search_hits = Signal(int, int, object)        # req id, page, list of rect lists
    search_progress = Signal(int, int, int)
    search_done = Signal(int, bool)
    _incoming = Signal(object)

    def __init__(self, name: str, parent=None):
        super().__init__(parent)
        self.name = name
        self._docs: dict = {}         # doc_key -> (version, bytes provider)
        self._loaded: dict = {}       # doc_key -> version loaded in the worker
        self._queue: list[Request] = []
        self._inflight: Request | None = None
        self._search_active: int | None = None
        self._proc = None
        self._conn = None
        self._reader = None
        self._alive = False
        self._incoming.connect(self._on_message)
        self._watchdog = QTimer(self)
        self._watchdog.setInterval(250)
        self._watchdog.timeout.connect(self._check_stuck)
        self._watchdog.start()
        self._start()

    # ----- process management -------------------------------------------------

    def _start(self):
        ctx = mp.get_context("spawn")
        parent, child = ctx.Pipe()
        self._proc = ctx.Process(target=worker.main, args=(child,), daemon=True, name=f"docket-{self.name}")
        self._proc.start()
        child.close()
        self._conn = parent
        self._loaded.clear()
        self._alive = True
        gen = object()
        self._gen = gen
        self._reader = threading.Thread(target=self._read_loop, args=(parent, gen), daemon=True)
        self._reader.start()

    def _read_loop(self, conn, gen):
        while True:
            try:
                msg = conn.recv()
            except (EOFError, OSError):
                break
            if gen is not self._gen:
                break
            self._incoming.emit(msg)

    def restart(self):
        log.info("restarting %s worker", self.name)
        self._gen = None
        try:
            self._proc.kill()
        except Exception:
            pass
        try:
            self._conn.close()
        except Exception:
            pass
        if self._inflight is not None and self._inflight.kind != "search":
            self._inflight = None
        if self._search_active is not None:
            self.search_done.emit(self._search_active, False)
            self._search_active = None
            self._inflight = None
        self._start()
        self._pump()

    def shutdown(self):
        self._watchdog.stop()
        self._gen = None
        try:
            self._conn.send(("quit",))
        except Exception:
            pass
        try:
            self._proc.join(0.5)
            if self._proc.is_alive():
                self._proc.kill()
        except Exception:
            pass

    # ----- documents --------------------------------------------------------------

    def set_document(self, key, version, data_fn):
        """Register the current version of a document. ``data_fn`` returns bytes."""
        self._docs[key] = (version, data_fn)

    def drop_document(self, key):
        self._docs.pop(key, None)
        if key in self._loaded:
            self._loaded.pop(key, None)
            self._send(("close", key))
        self._queue = [r for r in self._queue if r.doc_key != key]

    def _send(self, msg):
        try:
            self._conn.send(msg)
            return True
        except Exception:
            log.exception("send to %s worker failed", self.name)
            self.restart()
            return False

    def _ensure_loaded(self, req: Request) -> bool:
        entry = self._docs.get(req.doc_key)
        if not entry or entry[0] != req.version:
            return False  # stale request
        if self._loaded.get(req.doc_key) != req.version:
            if not self._send(("load", req.doc_key, req.version, entry[1]())):
                return False
            self._loaded[req.doc_key] = req.version
        return True

    # ----- scheduling -----------------------------------------------------------------

    def want(self, requests: list[Request], keep_others=False):
        """Replace the queue with ``requests`` (highest priority first)."""
        if keep_others:
            seen = {r.cache_key for r in requests}
            requests = requests + [r for r in self._queue if r.cache_key not in seen]
        if self._inflight is not None:
            requests = [r for r in requests if r.cache_key != self._inflight.cache_key]
        self._queue = requests
        self._pump()

    def is_pending(self, cache_key) -> bool:
        if self._inflight is not None and self._inflight.cache_key == cache_key:
            return True
        return any(r.cache_key == cache_key for r in self._queue)

    def _pump(self):
        while self._inflight is None and self._queue:
            req = self._queue.pop(0)
            if not self._ensure_loaded(req):
                continue
            if req.kind == "search":
                ok = self._send(("search", req.id, req.doc_key, req.version, req.extra[0],
                                 req.extra[1], req.page))
                self._search_active = req.id
            else:
                ok = self._send((req.kind, req.id, req.doc_key, req.version, req.page, req.zoom,
                                 req.clip))
            if ok:
                req.sent_at = time.monotonic()
                self._inflight = req

    def _check_stuck(self):
        r = self._inflight
        if r is None or r.kind == "search" or not self._alive:
            return
        if time.monotonic() - r.sent_at > STUCK_AFTER and self._queue and \
                not any(q.cache_key == r.cache_key for q in self._queue) and r.extra != "keep":
            # the user has moved on and this page is slow: don't make them wait
            self.restart()

    # ----- search -------------------------------------------------------------------------

    def start_search(self, doc_key, version, query, match_case, start_page) -> int:
        self.cancel_search()
        req = Request("search", doc_key, version, start_page, extra=(query, match_case))
        self._queue.insert(0, req)
        self._pump()
        return req.id

    def cancel_search(self):
        if self._search_active is not None:
            self._send(("cancel", self._search_active))
        self._queue = [r for r in self._queue if r.kind != "search"]

    # ----- results --------------------------------------------------------------------------

    def _on_message(self, msg):
        kind = msg[0]
        if kind == "img":
            _, rid, w, h, stride, samples = msg
            req = self._inflight
            self._inflight = None
            if req is not None and req.id == rid:
                img = QImage(samples, w, h, stride, QImage.Format.Format_RGB888).copy()
                self.image_ready.emit(req.cache_key, img)
            self._pump()
        elif kind == "fail" or kind == "error":
            if kind == "error":
                log.warning("%s worker: %s", self.name, msg[2])
            if self._inflight is not None and self._inflight.id == msg[1]:
                if self._inflight.kind == "search":
                    self._search_active = None
                    self.search_done.emit(msg[1], False)
                self._inflight = None
            self._pump()
        elif kind == "hits":
            self.search_hits.emit(msg[1], msg[2], msg[3])
        elif kind == "search_progress":
            self.search_progress.emit(msg[1], msg[2], msg[3])
        elif kind == "search_done":
            if self._inflight is not None and self._inflight.id == msg[1]:
                self._inflight = None
            if self._search_active == msg[1]:
                self._search_active = None
            self.search_done.emit(msg[1], msg[2])
            self._pump()


class Renderer(QObject):
    """Two workers: one for the page view, one for thumbnails and search."""

    image_ready = Signal(object, object)

    def __init__(self, parent=None):
        super().__init__(parent)
        self.view = RenderClient("view", self)
        self.side = RenderClient("side", self)
        self.cache = ImageCache()
        self.thumbs = ImageCache(80 * 1024 * 1024)
        self.view.image_ready.connect(self._store)
        self.side.image_ready.connect(self._store_thumb)

    def _store(self, key, img):
        self.cache.put(key, img)
        self.image_ready.emit(key, img)

    def _store_thumb(self, key, img):
        self.thumbs.put(key, img)
        self.image_ready.emit(key, img)

    def set_document(self, key, version, data_fn):
        cached = {}

        def once():
            if "b" not in cached:
                cached["b"] = data_fn()
            return cached["b"]

        self.view.set_document(key, version, once)
        self.side.set_document(key, version, once)

    def drop_document(self, key):
        self.view.drop_document(key)
        self.side.drop_document(key)

    def shutdown(self):
        self.view.shutdown()
        self.side.shutdown()
