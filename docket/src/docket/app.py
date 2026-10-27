"""Application entry point: logging, single instance, theme, main window."""

from __future__ import annotations

import getpass
import logging
import logging.handlers
import multiprocessing as mp
import os
import sys

from . import APP_ID, APP_NAME, ORG_NAME, __version__
from .paths import log_dir

log = logging.getLogger("docket")


def setup_logging():
    handler = logging.handlers.RotatingFileHandler(os.path.join(log_dir(), "docket.log"), maxBytes=2_000_000,
                                                   backupCount=3, encoding="utf-8")
    handler.setFormatter(logging.Formatter("%(asctime)s %(levelname)s %(name)s: %(message)s"))
    root = logging.getLogger()
    root.setLevel(logging.INFO)
    root.addHandler(handler)
    if os.environ.get("DOCKET_DEBUG"):
        root.addHandler(logging.StreamHandler())

    def hook(etype, value, tb):
        log.error("unhandled exception", exc_info=(etype, value, tb))
        sys.__excepthook__(etype, value, tb)

    sys.excepthook = hook


def server_name() -> str:
    try:
        user = getpass.getuser()
    except Exception:
        user = "user"
    return f"{APP_ID}-{user}"


def forward_to_running(paths: list[str]) -> bool:
    """If another Docket window is running, hand it the files and return True."""
    from PySide6.QtNetwork import QLocalSocket

    sock = QLocalSocket()
    sock.connectToServer(server_name())
    if not sock.waitForConnected(300):
        return False
    payload = "\n".join(os.path.abspath(p) for p in paths) or "\n"
    sock.write(payload.encode("utf-8"))
    sock.flush()
    sock.waitForBytesWritten(1000)
    sock.disconnectFromServer()
    return True


def listen(window):
    from PySide6.QtNetwork import QLocalServer

    QLocalServer.removeServer(server_name())
    server = QLocalServer(window)
    server.setSocketOptions(QLocalServer.SocketOption.UserAccessOption)
    if not server.listen(server_name()):
        log.warning("single-instance server: %s", server.errorString())
        return None

    def incoming():
        conn = server.nextPendingConnection()
        if conn is None:
            return

        def read():
            if not conn.waitForReadyRead(1000) and not conn.bytesAvailable():
                pass
            data = bytes(conn.readAll()).decode("utf-8", "replace")
            paths = [p for p in data.splitlines() if p.strip()]
            conn.disconnectFromServer()
            if window.isMinimized():
                window.showNormal()
            window.raise_()
            window.activateWindow()
            if paths:
                window.open_paths(paths)

        conn.readyRead.connect(read)

    server.newConnection.connect(incoming)
    return server


def main(argv=None) -> int:
    mp.freeze_support()
    argv = list(sys.argv if argv is None else argv)
    setup_logging()
    log.info("%s %s starting", APP_NAME, __version__)
    if sys.platform == "win32":
        try:
            import ctypes

            ctypes.windll.shell32.SetCurrentProcessExplicitAppUserModelID(f"{ORG_NAME}.{APP_ID}")
        except Exception:
            pass

    from PySide6.QtCore import Qt, QTimer
    from PySide6.QtGui import QGuiApplication, QIcon
    from PySide6.QtWidgets import QApplication

    QGuiApplication.setHighDpiScaleFactorRoundingPolicy(Qt.HighDpiScaleFactorRoundingPolicy.PassThrough)
    app = QApplication(argv)
    app.setApplicationName(APP_NAME)
    app.setOrganizationName(ORG_NAME)
    app.setApplicationVersion(__version__)
    files = [a for a in argv[1:] if not a.startswith("-")]

    if not os.environ.get("DOCKET_MULTI_INSTANCE") and forward_to_running(files):
        return 0

    from .ui import settings, theme
    from .ui.mainwindow import MainWindow

    icon_path = os.path.join(os.path.dirname(__file__), "assets", "docket.ico")
    if os.path.exists(icon_path):
        app.setWindowIcon(QIcon(icon_path))
    theme.apply(app, settings.get("theme", "system") or "system")
    win = MainWindow()
    server = listen(win) if not os.environ.get("DOCKET_MULTI_INSTANCE") else None
    win.show()
    to_open = files
    if not to_open and settings.get("restore_session", False, bool):
        sess = settings.get("session", [])
        if isinstance(sess, str):
            sess = [sess]
        to_open = [p for p in (sess or []) if p and os.path.exists(p)]
    if to_open:
        QTimer.singleShot(0, lambda: win.open_paths(to_open))

    exit_ms = os.environ.get("DOCKET_EXIT_AFTER_MS")
    if exit_ms:
        def done():
            shot = os.environ.get("DOCKET_SMOKE_SCREENSHOT")
            if shot:
                win.grab().save(shot)
            tabs = win.all_tabs()
            log.info("smoke: %d tab(s); pages=%s", len(tabs), [t.doc.page_count for t in tabs])
            print(f"smoke ok: {len(tabs)} tab(s)", flush=True)
            for t in tabs:
                t.doc.dirty = False
            win.close()
        QTimer.singleShot(int(exit_ms), done)

    rc = app.exec()
    if server is not None:
        server.close()
    log.info("exit %s", rc)
    return rc


if __name__ == "__main__":
    sys.exit(main())
