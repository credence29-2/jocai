"""Line icons drawn from small inline SVGs and tinted to the current theme."""

from __future__ import annotations

from functools import lru_cache

from PySide6.QtCore import QByteArray, QRectF, Qt
from PySide6.QtGui import QColor, QIcon, QImage, QPainter, QPixmap
from PySide6.QtSvg import QSvgRenderer

_P = {
    "open": '<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>',
    "save": '<path d="M5 3h11l5 5v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2z"/><path d="M7 3v5h8V3"/><rect x="7" y="13" width="10" height="8"/>',
    "print": '<path d="M6 9V3h12v6"/><rect x="3" y="9" width="18" height="8" rx="2"/><path d="M6 14h12v7H6z"/>',
    "undo": '<path d="M9 14 4 9l5-5"/><path d="M4 9h11a5 5 0 0 1 0 10h-3"/>',
    "redo": '<path d="m15 14 5-5-5-5"/><path d="M20 9H9a5 5 0 0 0 0 10h3"/>',
    "search": '<circle cx="11" cy="11" r="7"/><path d="m21 21-4.3-4.3"/>',
    "zoom_in": '<circle cx="11" cy="11" r="7"/><path d="m21 21-4.3-4.3M11 8v6M8 11h6"/>',
    "zoom_out": '<circle cx="11" cy="11" r="7"/><path d="m21 21-4.3-4.3M8 11h6"/>',
    "fit_page": '<rect x="6" y="3" width="12" height="18" rx="1"/><path d="M9 8h6M9 12h6M9 16h4"/>',
    "fit_width": '<path d="M3 12h18M7 8l-4 4 4 4M17 8l4 4-4 4"/>',
    "select": '<path d="M5 3l14 8-6 2-2 6z"/>',
    "text_select": '<path d="M8 4h3M13 4h3M12 4v16M8 20h3M13 20h3"/>',
    "hand": '<path d="M8 13V5.5a1.5 1.5 0 0 1 3 0V12M11 11V4a1.5 1.5 0 0 1 3 0v7M14 11V5.5a1.5 1.5 0 0 1 3 0V13M17 9a1.5 1.5 0 0 1 3 0v5a7 7 0 0 1-7 7h-1a7 7 0 0 1-5.7-3L3.6 14.6a1.5 1.5 0 0 1 2.3-1.9L8 15"/>',
    "highlight": '<path d="m9 11-6 6v3h9l3-3"/><path d="m22 12-4.6 4.6a2 2 0 0 1-2.8 0l-5.2-5.2a2 2 0 0 1 0-2.8L14 4"/>',
    "underline": '<path d="M6 4v6a6 6 0 0 0 12 0V4M4 20h16"/>',
    "strike": '<path d="M16 4H9a3 3 0 0 0-2.8 4M14 12a4 4 0 0 1 0 8H6M4 12h16"/>',
    "note": '<path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/>',
    "textbox": '<rect x="3" y="4" width="18" height="16" rx="1"/><path d="M8 9h8M12 9v7"/>',
    "addtext": '<path d="M5 6V4h14v2M12 4v16M9 20h6"/>',
    "pen": '<path d="M12 20h9"/><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z"/>',
    "rect": '<rect x="3" y="5" width="18" height="14" rx="1"/>',
    "oval": '<ellipse cx="12" cy="12" rx="9" ry="7"/>',
    "arrow": '<path d="M5 19 19 5M10 5h9v9"/>',
    "line": '<path d="M5 19 19 5"/>',
    "sign": '<path d="M3 17c3-6 5-9 6-9 2 0-2 9 0 9 1.5 0 3-4 4.5-4s.5 3 2 3c1 0 2-1 3-2"/><path d="M3 21h18"/>',
    "stamp": '<path d="M5 22h14M19 18H5v-3a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2zM12 13V8"/><circle cx="12" cy="5" r="3"/>',
    "redact": '<rect x="3" y="6" width="18" height="12" rx="1"/><path d="M7 10h10M7 14h6" stroke-width="3"/>',
    "shield": '<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>',
    "lock": '<rect x="4" y="11" width="16" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 8 0v4"/>',
    "unlock": '<rect x="4" y="11" width="16" height="10" rx="2"/><path d="M8 11V7a4 4 0 0 1 7.5-2"/>',
    "compress": '<path d="M4 14h6v6M20 10h-6V4M14 10l7-7M3 21l7-7"/>',
    "pages": '<rect x="3" y="3" width="7" height="9" rx="1"/><rect x="14" y="3" width="7" height="9" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/>',
    "rotate_left": '<path d="M3 12a9 9 0 1 0 3-6.7L3 8"/><path d="M3 3v5h5"/>',
    "rotate_right": '<path d="M21 12a9 9 0 1 1-3-6.7L21 8"/><path d="M21 3v5h-5"/>',
    "delete": '<path d="M3 6h18M8 6V4h8v2M19 6l-1 14H6L5 6M10 11v6M14 11v6"/>',
    "insert": '<path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><path d="M14 3v6h6M12 12v6M9 15h6"/>',
    "extract": '<path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><path d="M14 3v6h6M12 18v-6M9 15l3-3 3 3"/>',
    "merge": '<path d="M8 3v5a4 4 0 0 0 4 4 4 4 0 0 1 4 4v5M16 3v5a4 4 0 0 1-4 4"/>',
    "split": '<path d="M16 3h5v5M8 3H3v5M12 22v-8l-9-11M12 14l9-11"/>',
    "image": '<rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-5-5L5 21"/>',
    "export": '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4M7 10l5 5 5-5M12 15V3"/>',
    "field": '<rect x="3" y="7" width="18" height="10" rx="1"/><path d="M7 10v4"/>',
    "checkbox": '<rect x="4" y="4" width="16" height="16" rx="2"/><path d="m8 12 3 3 5-6"/>',
    "date": '<rect x="3" y="5" width="18" height="16" rx="2"/><path d="M16 3v4M8 3v4M3 10h18"/>',
    "wand": '<path d="m15 4-1-1M20 9l1 1M17.8 6.2 19 5M3 21l12-12M14 3v2M19 10h2M15 9l-1-1"/>',
    "flatten": '<path d="M12 2 2 7l10 5 10-5z"/><path d="m2 17 10 5 10-5M2 12l10 5 10-5"/>',
    "bookmark": '<path d="M19 21l-7-5-7 5V5a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2z"/>',
    "comments": '<path d="M21 11.5a8.4 8.4 0 0 1-9 8.4 8.5 8.5 0 0 1-3.8-.9L3 21l1.9-5.7A8.4 8.4 0 0 1 12 3a8.4 8.4 0 0 1 9 8.5z"/>',
    "thumbs": '<rect x="4" y="3" width="7" height="8" rx="1"/><rect x="13" y="3" width="7" height="8" rx="1"/><rect x="4" y="13" width="7" height="8" rx="1"/><rect x="13" y="13" width="7" height="8" rx="1"/>',
    "tools": '<rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><path d="M17.5 14v7M14 17.5h7"/>',
    "edit": '<path d="M12 20h9"/><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z"/>',
    "close": '<path d="M18 6 6 18M6 6l12 12"/>',
    "plus": '<path d="M12 5v14M5 12h14"/>',
    "chev_left": '<path d="m15 18-6-6 6-6"/>',
    "chev_right": '<path d="m9 18 6-6-6-6"/>',
    "chev_up": '<path d="m18 15-6-6-6 6"/>',
    "chev_down": '<path d="m6 9 6 6 6-6"/>',
    "info": '<circle cx="12" cy="12" r="9"/><path d="M12 16v-4M12 8h.01"/>',
    "file": '<path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><path d="M14 3v6h6"/>',
    "new": '<path d="M14 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/><path d="M14 3v6h6M12 12v6M9 15h6"/>',
    "comment_tool": '<path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/><path d="M8 9h8M8 13h5"/>',
    "form": '<rect x="4" y="3" width="16" height="18" rx="2"/><path d="M8 8h8M8 12h8M8 16h4"/>',
    "palette": '<circle cx="12" cy="12" r="9"/><circle cx="8" cy="10" r="1"/><circle cx="12" cy="7" r="1"/><circle cx="16" cy="10" r="1"/>',
    "eye": '<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/>',
    "menu": '<path d="M4 6h16M4 12h16M4 18h16"/>',
    "blank_page": '<rect x="5" y="3" width="14" height="18" rx="1"/>',
    "check": '<path d="M20 6 9 17l-5-5"/>',
    "moon": '<path d="M21 12.8A9 9 0 1 1 11.2 3a7 7 0 0 0 9.8 9.8z"/>',
    "ocr": '<path d="M3 7V5a2 2 0 0 1 2-2h2M17 3h2a2 2 0 0 1 2 2v2M21 17v2a2 2 0 0 1-2 2h-2M7 21H5a2 2 0 0 1-2-2v-2M8 9h8M8 12h8M8 15h5"/>',
}


@lru_cache(maxsize=512)
def _pixmap(name: str, size: int, color: str, dpr: float) -> QPixmap:
    body = _P.get(name, _P["file"])
    svg = (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" '
           f'stroke="{color}" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">'
           f'{body}</svg>')
    r = QSvgRenderer(QByteArray(svg.encode()))
    px = int(size * dpr)
    img = QImage(px, px, QImage.Format.Format_ARGB32_Premultiplied)
    img.fill(Qt.GlobalColor.transparent)
    p = QPainter(img)
    p.setRenderHint(QPainter.RenderHint.Antialiasing)
    r.render(p, QRectF(0, 0, px, px))
    p.end()
    pm = QPixmap.fromImage(img)
    pm.setDevicePixelRatio(dpr)
    return pm


_theme = {"fg": "#2b2b2b", "disabled": "#a0a0a0", "accent": "#1a73e8"}


def set_colors(fg: str, disabled: str, accent: str):
    _theme.update(fg=fg, disabled=disabled, accent=accent)


def icon(name: str, size: int = 20, color: str | None = None) -> QIcon:
    from PySide6.QtGui import QGuiApplication

    dpr = QGuiApplication.instance().devicePixelRatio() if QGuiApplication.instance() else 1.0
    ic = QIcon()
    ic.addPixmap(_pixmap(name, size, color or _theme["fg"], dpr), QIcon.Mode.Normal)
    ic.addPixmap(_pixmap(name, size, _theme["disabled"], dpr), QIcon.Mode.Disabled)
    ic.addPixmap(_pixmap(name, size, _theme["accent"], dpr), QIcon.Mode.Normal, QIcon.State.On)
    return ic


def color_swatch(color: QColor, size: int = 16) -> QIcon:
    pm = QPixmap(size, size)
    pm.fill(Qt.GlobalColor.transparent)
    p = QPainter(pm)
    p.setRenderHint(QPainter.RenderHint.Antialiasing)
    p.setBrush(color)
    p.setPen(QColor(0, 0, 0, 80))
    p.drawEllipse(1, 1, size - 2, size - 2)
    p.end()
    return QIcon(pm)


def set(obj, name: str, size: int = 20):
    """Give a QAction or button an icon that follows theme changes (see refresh)."""
    obj.setProperty("docketIcon", f"{name}:{size}")
    obj.setIcon(icon(name, size))


def refresh(root):
    """Re-tint every icon set with ``set`` under ``root`` after a theme change."""
    from PySide6.QtGui import QAction
    from PySide6.QtWidgets import QAbstractButton, QLabel

    for obj in [root] + root.findChildren(QAction) + root.findChildren(QAbstractButton) + root.findChildren(QLabel):
        spec = obj.property("docketIcon")
        if not spec:
            continue
        name, size = spec.rsplit(":", 1)
        if isinstance(obj, QLabel):
            obj.setPixmap(icon(name, int(size)).pixmap(int(size), int(size)))
        else:
            obj.setIcon(icon(name, int(size)))
