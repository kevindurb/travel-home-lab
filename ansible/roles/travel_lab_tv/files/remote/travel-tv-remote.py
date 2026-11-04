#!/usr/bin/python3
"""Travel TV phone-remote API.

A tiny stdlib HTTP server that turns taps on the phone page into Wayland
input events on the kiosk. It listens on a Unix socket (no TCP), and the
travel-tv nginx container proxies /api/ to it.

Keyboard and text go through wtype; pointer events go through wlrctl; volume
goes through wpctl. Every helper is invoked without a shell and every value
is checked against a small allowlist.
"""

from __future__ import annotations

import argparse
import glob
import json
import os
import re
import shutil
import socketserver
import stat
import subprocess
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler

KEYS = {
    "Up", "Down", "Left", "Right",
    "Return", "Escape", "BackSpace", "space", "Tab",
    "Home", "End", "Page_Up", "Page_Down",
}
MODIFIERS = {"alt", "ctrl", "shift", "logo"}
BUTTONS = {"left", "right", "middle"}

KEY_LOCK = threading.Lock()
SINK_LOCK = threading.Lock()
ERROR_LOCK = threading.Lock()

SETTLE_SECONDS = max(
    0.5, int(os.environ.get("TRAVEL_TV_REMOTE_SETTLE_MS", "2000")) / 1000.0
)

LAST_ERROR = None
SINK_CACHE = {"at": 0.0, "id": None, "name": "", "description": ""}


class BusyError(Exception):
    """Another launch sequence is already running."""


def set_error(message):
    global LAST_ERROR
    with ERROR_LOCK:
        LAST_ERROR = message
    if message:
        sys.stderr.write("travel-tv-remote: {}\n".format(message))


def last_error():
    with ERROR_LOCK:
        return LAST_ERROR


def runtime_dir():
    return os.environ.get("XDG_RUNTIME_DIR") or "/run/user/{}".format(os.getuid())


def find_wayland():
    """Return the Wayland display name for the kiosk, or None."""
    override = os.environ.get("WAYLAND_DISPLAY")
    if override:
        path = override if override.startswith("/") else os.path.join(runtime_dir(), override)
        if os.path.exists(path):
            return override

    candidates = []
    for path in glob.glob(os.path.join(runtime_dir(), "wayland-*")):
        try:
            if stat.S_ISSOCK(os.stat(path).st_mode):
                candidates.append(path)
        except OSError:
            continue
    if not candidates:
        return None
    candidates.sort(key=os.path.getmtime, reverse=True)
    return os.path.basename(candidates[0])


def tool(name):
    return shutil.which(name) or "/usr/bin/{}".format(name)


def base_env():
    env = os.environ.copy()
    env["XDG_RUNTIME_DIR"] = runtime_dir()
    return env


def run_helper(args, env, timeout):
    try:
        proc = subprocess.run(
            args,
            env=env,
            capture_output=True,
            text=True,
            timeout=timeout,
        )
    except FileNotFoundError:
        raise RuntimeError("{} is not installed".format(os.path.basename(args[0])))
    except subprocess.TimeoutExpired:
        raise RuntimeError("{} timed out".format(os.path.basename(args[0])))
    if proc.returncode != 0:
        lines = (proc.stderr or proc.stdout or "").strip().splitlines()
        raise RuntimeError(lines[-1] if lines else "{} failed".format(os.path.basename(args[0])))
    return proc.stdout


def run_wayland(args, timeout=4):
    display = find_wayland()
    if not display:
        raise RuntimeError("TV is offline (no Wayland display)")
    env = base_env()
    env["WAYLAND_DISPLAY"] = display
    return run_helper(args, env, timeout)


def run_pipewire(args, timeout=5):
    return run_helper(args, base_env(), timeout)


def press(name, repeat=1):
    for index in range(repeat):
        run_wayland([tool("wtype"), "-k", name])
        if index + 1 < repeat:
            time.sleep(0.08)


def press_combo(mod, name):
    run_wayland([tool("wtype"), "-M", mod, "-k", name, "-m", mod])
    time.sleep(0.15)


def type_text(text):
    run_wayland([tool("wtype"), "--", text])


def launch(index, columns):
    """Open the launcher, walk the grid, and press Enter."""
    if not KEY_LOCK.acquire(blocking=False):
        raise BusyError()
    try:
        press_combo("alt", "Home")
        time.sleep(SETTLE_SECONDS)
        row, column = divmod(index, columns)
        for _ in range(row):
            press("Down")
        for _ in range(column):
            press("Right")
        press("Return")
    finally:
        KEY_LOCK.release()


def pointer(dx, dy):
    if not dx and not dy:
        return
    run_wayland([tool("wlrctl"), "pointer", "move", str(dx), str(dy)])


def click(button):
    args = [tool("wlrctl"), "pointer", "click"]
    if button != "left":
        args.append(button)
    run_wayland(args)


def scroll(dy, dx):
    if not dx and not dy:
        return
    run_wayland([tool("wlrctl"), "pointer", "scroll", str(dy), str(dx)])


def list_sinks():
    output = run_pipewire([tool("pw-dump")])
    data = json.loads(output)
    sinks = []
    for obj in data:
        info = obj.get("info") or {}
        props = info.get("props") or {}
        if props.get("media.class") != "Audio/Sink":
            continue
        sinks.append(
            {
                "id": obj.get("id"),
                "name": props.get("node.name") or "",
                "description": props.get("node.description") or "",
            }
        )
    return sinks


def resolve_sink(force=False):
    """Prefer the HDMI sink; fall back to PipeWire's default sink."""
    now = time.monotonic()
    with SINK_LOCK:
        if not force and SINK_CACHE["id"] and (now - SINK_CACHE["at"]) < 20:
            return dict(SINK_CACHE)

    sinks = []
    try:
        sinks = list_sinks()
    except Exception:
        # No PipeWire, or pw-dump is unavailable. wpctl can still address the
        # default sink, so keep going.
        pass

    hdmi = next(
        (
            sink
            for sink in sinks
            if "hdmi" in "{} {}".format(sink["name"], sink["description"]).lower()
        ),
        None,
    )
    resolved = hdmi or {
        "id": "@DEFAULT_AUDIO_SINK@",
        "name": "",
        "description": "default",
    }
    with SINK_LOCK:
        SINK_CACHE.update(resolved)
        SINK_CACHE["at"] = now
        return dict(SINK_CACHE)


def volume_state():
    sink = resolve_sink()
    output = run_pipewire([tool("wpctl"), "get-volume", str(sink["id"])], timeout=2)
    match = re.search(r"Volume:\s*([0-9.]+)", output)
    return {
        "sink": sink["name"] or sink["description"] or "default",
        "volume": float(match.group(1)) if match else None,
        "muted": "[MUTED]" in output,
    }


def set_volume(value):
    sink = resolve_sink()
    percent = max(0, min(100, round(value * 100)))
    run_pipewire(
        [tool("wpctl"), "set-volume", str(sink["id"]), "{}%".format(percent)],
        timeout=2,
    )


def set_mute(muted):
    sink = resolve_sink()
    run_pipewire(
        [tool("wpctl"), "set-mute", str(sink["id"]), "1" if muted else "0"],
        timeout=2,
    )


def toggle_mute():
    state = volume_state()
    set_mute(not bool(state.get("muted")))


def clamp_int(value, low, high):
    try:
        number = int(value)
    except (TypeError, ValueError):
        raise ValueError("expected a number")
    return max(low, min(high, number))


def normalize_text(value):
    if value is None:
        return ""
    return " ".join(str(value).split())[:300]


def dispatch(payload):
    if not isinstance(payload, dict):
        raise ValueError("expected a JSON object")

    kind = payload.get("type")
    if kind == "key":
        name = payload.get("key")
        if name not in KEYS:
            raise ValueError("unsupported key")
        repeat = clamp_int(payload.get("repeat", 1), 1, 10)
        with KEY_LOCK:
            press(name, repeat)
    elif kind == "combo":
        mod = payload.get("mod")
        name = payload.get("key")
        if mod not in MODIFIERS:
            raise ValueError("unsupported modifier")
        if name not in KEYS:
            raise ValueError("unsupported key")
        with KEY_LOCK:
            press_combo(mod, name)
    elif kind == "text":
        text = normalize_text(payload.get("text"))
        if not text:
            raise ValueError("empty text")
        with KEY_LOCK:
            type_text(text)
    elif kind == "pointer":
        pointer(
            clamp_int(payload.get("dx", 0), -500, 500),
            clamp_int(payload.get("dy", 0), -500, 500),
        )
    elif kind == "click":
        button = payload.get("button", "left")
        if button not in BUTTONS:
            raise ValueError("unsupported button")
        click(button)
    elif kind == "scroll":
        scroll(
            clamp_int(payload.get("dy", 0), -50, 50),
            clamp_int(payload.get("dx", 0), -50, 50),
        )
    elif kind == "launch":
        launch(
            clamp_int(payload.get("index"), 0, 99),
            clamp_int(payload.get("columns"), 1, 8),
        )
    elif kind == "volume":
        try:
            value = float(payload.get("value"))
        except (TypeError, ValueError):
            raise ValueError("invalid volume")
        if not 0 <= value <= 1:
            raise ValueError("volume out of range")
        set_volume(value)
    elif kind == "mute":
        if payload.get("toggle"):
            toggle_mute()
        elif "muted" in payload:
            set_mute(bool(payload.get("muted")))
        else:
            raise ValueError("missing mute value")
    else:
        raise ValueError("unknown action")


def health():
    display = find_wayland()
    wtype_ok = bool(shutil.which("wtype"))
    wlrctl_ok = bool(shutil.which("wlrctl"))
    result = {
        "ok": bool(display and wtype_ok and wlrctl_ok),
        "wayland": display,
        "wtype": wtype_ok,
        "wlrctl": wlrctl_ok,
        "sink": None,
        "volume": None,
        "muted": None,
        "last_error": last_error(),
    }
    try:
        result.update(volume_state())
    except Exception as exc:
        result["last_error"] = str(exc)
    return result


class Handler(BaseHTTPRequestHandler):
    server_version = "TravelTVRemote/1.0"
    timeout = 10

    def _json(self, status, payload):
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path.split("?", 1)[0] == "/health":
            return self._json(200, health())
        return self._json(404, {"ok": False, "error": "not found"})

    def do_POST(self):
        if self.path.split("?", 1)[0] != "/action":
            return self._json(404, {"ok": False, "error": "not found"})

        try:
            length = int(self.headers.get("Content-Length", "0"))
        except ValueError:
            return self._json(400, {"ok": False, "error": "invalid body size"})
        if length <= 0 or length > 8192:
            return self._json(400, {"ok": False, "error": "invalid body size"})

        try:
            payload = json.loads(self.rfile.read(length).decode("utf-8"))
            dispatch(payload)
        except BusyError:
            return self._json(409, {"ok": False, "error": "another launch is in progress"})
        except (ValueError, TypeError, json.JSONDecodeError) as exc:
            return self._json(400, {"ok": False, "error": str(exc) or "invalid request"})
        except Exception as exc:
            set_error(str(exc))
            return self._json(503, {"ok": False, "error": str(exc)})

        set_error(None)
        return self._json(200, {"ok": True})

    def log_message(self, fmt, *args):
        # Pointer moves can be frequent; only failures are logged (by
        # set_error), not every successful request.
        pass


class Server(socketserver.ThreadingUnixStreamServer):
    daemon_threads = True

    def __init__(self, path, handler):
        self._socket_path = path
        super().__init__(path, handler)

    def server_bind(self):
        try:
            os.unlink(self._socket_path)
        except FileNotFoundError:
            pass
        super().server_bind()
        # The rootless nginx worker is a mapped subuid, so let any local
        # container user connect. The socket lives in the user's own config
        # directory and is never exposed over TCP.
        os.chmod(self._socket_path, 0o666)

    def get_request(self):
        request, _ = super().get_request()
        return request, ("local", 0)


def default_socket():
    return os.path.expanduser("~/.config/travel-tv/run/remote.sock")


def main():
    parser = argparse.ArgumentParser(description="Travel TV phone-remote API")
    parser.add_argument(
        "--socket",
        default=os.environ.get("TRAVEL_TV_REMOTE_SOCKET") or default_socket(),
    )
    args = parser.parse_args()

    os.makedirs(os.path.dirname(args.socket), exist_ok=True)
    server = Server(args.socket, Handler)
    sys.stderr.write("travel-tv-remote: listening on {}\n".format(args.socket))
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()
        try:
            os.unlink(args.socket)
        except FileNotFoundError:
            pass


if __name__ == "__main__":
    main()
