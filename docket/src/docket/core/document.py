"""In-memory PDF document with undo/redo and safe saving.

Documents are read fully into memory when opened, so the file is never locked on
disk. Every edit goes through ``edit()``, which snapshots the whole document first,
so undo is simply "reload the previous snapshot".

Each page carries a stable id that survives reordering and changes when the page's
content changes. The renderer caches by page id, so moving pages around costs
nothing and editing one page only re-renders that page.
"""

from __future__ import annotations

import itertools
import os
import secrets
import tempfile
from contextlib import contextmanager
from dataclasses import dataclass, field

import pymupdf

MAX_UNDO_STEPS = 50
MAX_UNDO_BYTES = 500 * 1024 * 1024

_ids = itertools.count(1)


def new_page_id() -> int:
    return next(_ids)


class PasswordRequired(Exception):
    """The file needs a password (or the one given was wrong)."""


class NotAPdf(Exception):
    pass


@dataclass
class Security:
    """Encryption to apply when saving. ``None`` fields mean no password."""

    user_pw: str | None = None
    owner_pw: str | None = None
    permissions: int = -1  # -1 = everything allowed

    @property
    def encrypted(self) -> bool:
        return bool(self.user_pw or self.owner_pw)

    @property
    def restricted(self) -> bool:
        return self.permissions != -1 and (self.permissions & ALL_PERMS) != ALL_PERMS


ALL_PERMS = (
    pymupdf.PDF_PERM_PRINT
    | pymupdf.PDF_PERM_MODIFY
    | pymupdf.PDF_PERM_COPY
    | pymupdf.PDF_PERM_ANNOTATE
    | pymupdf.PDF_PERM_FORM
    | pymupdf.PDF_PERM_ACCESSIBILITY
    | pymupdf.PDF_PERM_ASSEMBLE
    | pymupdf.PDF_PERM_PRINT_HQ
)


@dataclass
class _Snapshot:
    data: bytes
    page_ids: list[int]
    security: Security
    label: str


@dataclass
class OpenInfo:
    """How the file was protected when it was opened."""

    had_open_password: bool = False
    opened_as_owner: bool = False
    restricted: bool = False
    signed: bool = False
    extra: dict = field(default_factory=dict)


def _authenticate(doc: pymupdf.Document, password: str | None) -> int:
    # Never read needs_pass after authenticating: in some MuPDF versions that
    # resets the crypt state and the next save writes undecryptable content.
    if not doc.needs_pass:
        if password:
            return doc.authenticate(password)
        return 1
    if not password:
        raise PasswordRequired()
    rc = doc.authenticate(password)
    if not rc:
        raise PasswordRequired()
    return rc


class Document:
    """An open PDF. Not thread-safe; use from the UI thread only."""

    def __init__(self, data: bytes, path: str | None = None, password: str | None = None,
                 title: str | None = None):
        try:
            doc = pymupdf.open(stream=data, filetype="pdf")
        except Exception as exc:  # MuPDF raises a variety of types here
            raise NotAPdf(str(exc)) from exc
        if not doc.is_pdf:
            raise NotAPdf("Not a PDF file")
        had_pw = bool(doc.needs_pass)
        rc = _authenticate(doc, password)
        perms = doc.permissions
        restricted = (perms & ALL_PERMS) != ALL_PERMS
        self.info = OpenInfo(
            had_open_password=had_pw,
            opened_as_owner=bool(rc & 4),
            restricted=restricted,
        )
        self.security = Security()
        if had_pw or restricted:
            # Keep the protection on save. If we only know the user password,
            # the owner password is unknown, so a random one keeps the
            # restrictions in force.
            owner = password if rc & 4 else None
            self.security = Security(
                user_pw=password if had_pw else None,
                owner_pw=owner or (secrets.token_urlsafe(24) if restricted else None),
                permissions=perms if restricted else -1,
            )
        self.info.extra["owner_unknown"] = restricted and not (rc & 4)
        # Work on a decrypted copy so snapshots and workers never need passwords.
        plain = doc.tobytes() if (had_pw or restricted) else data
        self._doc = pymupdf.open(stream=plain, filetype="pdf") if plain is not data else doc
        self.path = path
        self.title = title or (os.path.basename(path) if path else "Untitled.pdf")
        self.page_ids = [new_page_id() for _ in range(self._doc.page_count)]
        self.version = 0
        self._undo: list[_Snapshot] = []
        self._redo: list[_Snapshot] = []
        self.dirty = False
        self.info.signed = self._has_signatures()
        self.listeners: list = []

    # ----- construction helpers -------------------------------------------------

    @classmethod
    def open_file(cls, path: str, password: str | None = None) -> "Document":
        with open(path, "rb") as fh:
            data = fh.read()
        return cls(data, path=path, password=password)

    @classmethod
    def from_pymupdf(cls, doc: pymupdf.Document, title: str) -> "Document":
        d = cls(doc.tobytes(garbage=1), title=title)
        d.dirty = True
        return d

    @classmethod
    def blank(cls, width: float, height: float, count: int = 1, title="Untitled.pdf") -> "Document":
        doc = pymupdf.open()
        for _ in range(count):
            doc.new_page(width=width, height=height)
        return cls.from_pymupdf(doc, title)

    # ----- access ----------------------------------------------------------------

    @property
    def pdf(self) -> pymupdf.Document:
        """The underlying PyMuPDF document. Core modules only."""
        return self._doc

    @property
    def page_count(self) -> int:
        return self._doc.page_count

    def page(self, index: int) -> pymupdf.Page:
        return self._doc[index]

    def page_size(self, index: int) -> tuple[float, float]:
        r = self._doc[index].rect  # already rotated
        return r.width, r.height

    def page_sizes(self) -> list[tuple[float, float]]:
        return [self.page_size(i) for i in range(self.page_count)]

    def geometry(self, index: int) -> tuple[float, float, tuple, tuple]:
        """(width, height, rotation matrix, derotation matrix) for a page. Width and
        height are as displayed (rotated). The rotation matrix maps the unrotated
        coordinates used by text, annotations and fields onto the displayed page."""
        p = self._doc[index]
        r = p.rect
        return r.width, r.height, tuple(p.rotation_matrix), tuple(p.derotation_matrix)

    def tobytes(self) -> bytes:
        return self._doc.tobytes()

    def _has_signatures(self) -> bool:
        try:
            return bool(self._doc.get_sigflags() > 0)
        except Exception:
            return False

    # ----- editing -----------------------------------------------------------------

    @contextmanager
    def edit(self, label: str, pages: list[int] | None = None, structure: bool = False):
        """Wrap a change. ``pages`` are the indices whose content changes (all if None
        and not ``structure``). Structural edits update ``page_ids`` themselves.
        If the body raises, the document is restored."""
        snap = self._snapshot(label)
        try:
            yield self._doc
        except BaseException:
            self._restore(snap)
            raise
        self._push_undo(snap)
        self._redo.clear()
        if not structure:
            targets = range(self.page_count) if pages is None else pages
            for i in targets:
                if 0 <= i < len(self.page_ids):
                    self.page_ids[i] = new_page_id()
        if len(self.page_ids) != self.page_count:  # defensive: keep ids aligned
            self.page_ids = (self.page_ids + [new_page_id() for _ in range(self.page_count)])[: self.page_count]
        self._changed()

    def _snapshot(self, label: str) -> _Snapshot:
        return _Snapshot(self._doc.tobytes(), list(self.page_ids),
                         Security(**vars(self.security)), label)

    def _restore(self, snap: _Snapshot):
        self._doc.close()
        self._doc = pymupdf.open(stream=snap.data, filetype="pdf")
        self.page_ids = list(snap.page_ids)
        self.security = Security(**vars(snap.security))

    def _push_undo(self, snap: _Snapshot):
        self._undo.append(snap)
        while len(self._undo) > MAX_UNDO_STEPS:
            self._undo.pop(0)
        total = sum(len(s.data) for s in self._undo)
        while total > MAX_UNDO_BYTES and len(self._undo) > 1:
            total -= len(self._undo.pop(0).data)

    def can_undo(self) -> bool:
        return bool(self._undo)

    def can_redo(self) -> bool:
        return bool(self._redo)

    def undo_label(self) -> str:
        return self._undo[-1].label if self._undo else ""

    def redo_label(self) -> str:
        return self._redo[-1].label if self._redo else ""

    def undo(self):
        if not self._undo:
            return
        snap = self._undo.pop()
        self._redo.append(self._snapshot(snap.label))
        self._restore(snap)
        self._changed()

    def redo(self):
        if not self._redo:
            return
        snap = self._redo.pop()
        self._undo.append(self._snapshot(snap.label))
        self._restore(snap)
        self._changed()

    def _changed(self):
        self.version += 1
        self.dirty = True
        for cb in list(self.listeners):
            cb(self)

    # ----- saving -------------------------------------------------------------------

    def save(self, path: str | None = None, *, garbage: int = 1) -> str:
        """Write to ``path`` (default: the current file) via a temp file and an atomic
        replace. Like Acrobat's Save As, the document then belongs to the new path."""
        target = path or self.path
        if not target:
            raise ValueError("No path to save to")
        data = self._encoded(garbage=garbage)
        folder = os.path.dirname(os.path.abspath(target)) or "."
        fd, tmp = tempfile.mkstemp(prefix=".docket-", suffix=".pdf", dir=folder)
        try:
            with os.fdopen(fd, "wb") as fh:
                fh.write(data)
                fh.flush()
                os.fsync(fh.fileno())
            _replace(tmp, target)
        except BaseException:
            try:
                os.unlink(tmp)
            except OSError:
                pass
            raise
        self.path = target
        self.title = os.path.basename(target)
        self.dirty = False
        return target

    def _encoded(self, garbage: int = 1) -> bytes:
        kw = dict(garbage=garbage, deflate=True)
        s = self.security
        if s.encrypted:
            kw.update(
                encryption=pymupdf.PDF_ENCRYPT_AES_256,
                user_pw=s.user_pw or "",
                owner_pw=s.owner_pw or s.user_pw or "",
                permissions=s.permissions if s.permissions != -1 else ALL_PERMS,
            )
        return self._doc.tobytes(**kw)

    def close(self):
        self.listeners.clear()
        try:
            self._doc.close()
        except Exception:
            pass


def _replace(src: str, dst: str):
    # os.replace is atomic on the same volume. On Windows it fails if another
    # program holds the target open without share-delete; retry briefly.
    import time

    for attempt in range(10):
        try:
            os.replace(src, dst)
            return
        except PermissionError:
            if attempt == 9:
                raise
            time.sleep(0.1)
