"""Password protection and permissions."""

from __future__ import annotations

from dataclasses import dataclass

import pymupdf

from .document import ALL_PERMS, Document, Security

PERM_LABELS = {
    "print": pymupdf.PDF_PERM_PRINT | pymupdf.PDF_PERM_PRINT_HQ,
    "copy": pymupdf.PDF_PERM_COPY | pymupdf.PDF_PERM_ACCESSIBILITY,
    "edit": pymupdf.PDF_PERM_MODIFY | pymupdf.PDF_PERM_ASSEMBLE,
    "comment": pymupdf.PDF_PERM_ANNOTATE | pymupdf.PDF_PERM_FORM,
}


@dataclass
class Status:
    encrypted: bool
    open_password: bool
    restricted: bool
    allowed: dict


def status(doc: Document) -> Status:
    s = doc.security
    perms = s.permissions if s.permissions != -1 else ALL_PERMS
    return Status(
        encrypted=s.encrypted,
        open_password=bool(s.user_pw),
        restricted=s.restricted,
        allowed={k: bool(perms & v) for k, v in PERM_LABELS.items()},
    )


def file_info(data: bytes) -> tuple[bool, bool]:
    """(needs open password, has restrictions) from raw bytes, using a fresh copy so
    the working document is never touched (see document._authenticate)."""
    d = pymupdf.open(stream=data, filetype="pdf")
    try:
        needs = bool(d.needs_pass)
        restricted = bool(d.is_encrypted) and not needs
        return needs, restricted
    finally:
        d.close()


def protect(doc: Document, open_password: str | None, permissions_password: str | None,
            allow: dict | None = None):
    """Set AES-256 protection; applied when the document is saved."""
    perms = -1
    if permissions_password and allow is not None:
        perms = 0
        for k, bits in PERM_LABELS.items():
            if allow.get(k, True):
                perms |= bits
        if perms == ALL_PERMS:
            perms = -1
    with doc.edit("Password Protect", pages=[]):
        doc.security = Security(
            user_pw=open_password or None,
            owner_pw=permissions_password or (open_password or None),
            permissions=perms,
        )


def check_permissions_password(doc: Document, pw: str) -> bool:
    return bool(pw) and pw == doc.security.owner_pw


def remove(doc: Document):
    with doc.edit("Remove Password", pages=[]):
        doc.security = Security()
