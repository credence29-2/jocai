"""Light and dark themes in an Acrobat-like style."""

from __future__ import annotations

from PySide6.QtCore import Qt
from PySide6.QtGui import QColor, QGuiApplication, QPalette
from PySide6.QtWidgets import QApplication

from . import icons

LIGHT = dict(
    window="#f5f5f5", base="#ffffff", alt="#fafafa", text="#1f1f1f", muted="#6e6e6e",
    border="#dcdcdc", panel="#ffffff", canvas="#e6e6e6", accent="#1473e6", accent_fg="#ffffff",
    hover="#ebebeb", pressed="#dedede", disabled="#a8a8a8", selection="#cfe2fb",
)
DARK = dict(
    window="#262626", base="#1d1d1d", alt="#2b2b2b", text="#ebebeb", muted="#a0a0a0",
    border="#3d3d3d", panel="#2c2c2c", canvas="#3a3a3a", accent="#378ef0", accent_fg="#ffffff",
    hover="#383838", pressed="#454545", disabled="#6b6b6b", selection="#1f3f66",
)

current = dict(LIGHT)


def is_dark(mode: str) -> bool:
    if mode == "dark":
        return True
    if mode == "light":
        return False
    hints = QGuiApplication.styleHints()
    try:
        return hints.colorScheme() == Qt.ColorScheme.Dark
    except Exception:
        return False


def apply(app: QApplication, mode: str = "system"):
    c = DARK if is_dark(mode) else LIGHT
    current.clear()
    current.update(c)
    app.setStyle("Fusion")
    pal = QPalette()
    roles = {
        QPalette.ColorRole.Window: c["window"], QPalette.ColorRole.WindowText: c["text"],
        QPalette.ColorRole.Base: c["base"], QPalette.ColorRole.AlternateBase: c["alt"],
        QPalette.ColorRole.Text: c["text"], QPalette.ColorRole.Button: c["panel"],
        QPalette.ColorRole.ButtonText: c["text"], QPalette.ColorRole.Highlight: c["accent"],
        QPalette.ColorRole.HighlightedText: c["accent_fg"], QPalette.ColorRole.ToolTipBase: c["panel"],
        QPalette.ColorRole.ToolTipText: c["text"], QPalette.ColorRole.PlaceholderText: c["muted"],
        QPalette.ColorRole.Link: c["accent"], QPalette.ColorRole.Mid: c["border"],
        QPalette.ColorRole.Light: c["panel"], QPalette.ColorRole.Midlight: c["hover"],
        QPalette.ColorRole.Dark: c["border"],
    }
    for role, col in roles.items():
        pal.setColor(role, QColor(col))
    for role in (QPalette.ColorRole.WindowText, QPalette.ColorRole.Text, QPalette.ColorRole.ButtonText):
        pal.setColor(QPalette.ColorGroup.Disabled, role, QColor(c["disabled"]))
    app.setPalette(pal)
    icons.set_colors(c["text"], c["disabled"], c["accent"])
    icons._pixmap.cache_clear()
    app.setStyleSheet(STYLE.format(**c))


STYLE = """
QMainWindow, QDialog {{ background: {window}; }}
QToolBar {{ background: {panel}; border: none; border-bottom: 1px solid {border}; spacing: 2px; padding: 3px 6px; }}
QToolBar#quick {{ border-bottom: 1px solid {border}; }}
QToolButton {{ border: 1px solid transparent; border-radius: 6px; padding: 4px; color: {text}; }}
QToolButton:hover {{ background: {hover}; }}
QToolButton:pressed {{ background: {pressed}; }}
QToolButton:checked {{ background: {selection}; border-color: {accent}; }}
QToolButton[popupMode="1"] {{ padding-right: 14px; }}
QTabWidget::pane {{ border: none; }}
QTabBar::tab {{ background: {window}; color: {muted}; padding: 7px 14px; border: none; border-right: 1px solid {border};
    min-width: 80px; max-width: 240px; }}
QTabBar::tab:selected {{ background: {panel}; color: {text}; border-top: 2px solid {accent}; }}
QTabBar::tab:hover:!selected {{ background: {hover}; }}
QStatusBar {{ background: {panel}; border-top: 1px solid {border}; color: {muted}; }}
QFrame#toolsPane, QFrame#rightPane, QFrame#rail {{ background: {panel}; border: none; }}
QFrame#toolsPane {{ border-right: 1px solid {border}; }}
QWidget#paneInner {{ background: {panel}; }}
QFrame#rightPane {{ border-left: 1px solid {border}; }}
QFrame#rail {{ border-left: 1px solid {border}; }}
QLabel#paneTitle {{ font-weight: 600; font-size: 14px; padding: 4px 2px; }}
QLabel#muted {{ color: {muted}; }}
QPushButton {{ background: {panel}; border: 1px solid {border}; border-radius: 14px; padding: 5px 14px; color: {text}; }}
QPushButton:hover {{ background: {hover}; }}
QPushButton:default, QPushButton#primary {{ background: {accent}; color: {accent_fg}; border-color: {accent}; }}
QPushButton:default:hover, QPushButton#primary:hover {{ background: {accent}; }}
QPushButton:disabled {{ color: {disabled}; background: {window}; border-color: {border}; }}
QPushButton#toolEntry {{ text-align: left; border: none; border-radius: 6px; padding: 7px 8px; background: transparent; }}
QPushButton#toolEntry:hover {{ background: {hover}; }}
QPushButton#toolEntry:checked {{ background: {selection}; }}
QLineEdit, QComboBox, QSpinBox, QDoubleSpinBox, QDateEdit, QPlainTextEdit, QTextEdit {{
    background: {base}; border: 1px solid {border}; border-radius: 4px; padding: 3px 5px; color: {text};
    selection-background-color: {accent}; }}
QLineEdit:focus, QComboBox:focus, QSpinBox:focus, QPlainTextEdit:focus {{ border-color: {accent}; }}
QListWidget, QTreeWidget, QTreeView, QListView {{ background: {panel}; border: none; color: {text}; outline: 0; }}
QListWidget::item:selected, QTreeWidget::item:selected, QTreeView::item:selected {{ background: {selection}; color: {text}; }}
QMenu {{ background: {panel}; border: 1px solid {border}; padding: 4px; }}
QMenu::item {{ padding: 5px 24px 5px 24px; border-radius: 4px; }}
QMenu::item:selected {{ background: {hover}; }}
QMenu::separator {{ height: 1px; background: {border}; margin: 4px 8px; }}
QMenuBar {{ background: {panel}; border-bottom: 1px solid {border}; }}
QMenuBar::item {{ padding: 4px 10px; background: transparent; }}
QMenuBar::item:selected {{ background: {hover}; border-radius: 4px; }}
QScrollBar:vertical {{ background: transparent; width: 12px; margin: 0; }}
QScrollBar::handle:vertical {{ background: {border}; border-radius: 5px; min-height: 30px; margin: 2px; }}
QScrollBar:horizontal {{ background: transparent; height: 12px; margin: 0; }}
QScrollBar::handle:horizontal {{ background: {border}; border-radius: 5px; min-width: 30px; margin: 2px; }}
QScrollBar::add-line, QScrollBar::sub-line {{ width: 0; height: 0; }}
QScrollBar::add-page, QScrollBar::sub-page {{ background: transparent; }}
QSplitter::handle {{ background: {border}; }}
QFrame#floatBar {{ background: {panel}; border: 1px solid {border}; border-radius: 8px; }}
QFrame#findBar {{ background: {panel}; border: 1px solid {border}; border-radius: 8px; }}
QFrame#contextBar {{ background: {panel}; border-bottom: 1px solid {border}; }}
QCheckBox, QRadioButton, QLabel, QGroupBox {{ color: {text}; }}
QGroupBox {{ border: 1px solid {border}; border-radius: 6px; margin-top: 10px; padding-top: 8px; }}
QGroupBox::title {{ subcontrol-origin: margin; left: 8px; padding: 0 4px; }}
QToolTip {{ background: {panel}; color: {text}; border: 1px solid {border}; padding: 4px; }}
"""
