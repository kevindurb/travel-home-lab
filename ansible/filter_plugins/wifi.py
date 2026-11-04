"""Ansible filters for the Travel TV Wi-Fi tile.

The Wi-Fi QR payload format is defined by the ZXing convention:

    WIFI:T:<WPA|WEP|nopass>;S:<ssid>;P:<psk>;H:<true|false>;;

Special characters in the SSID and PSK are backslash-escaped.
"""

from __future__ import annotations


def _escape(value: str) -> str:
    """Escape the characters that are special inside a WIFI: payload."""
    return "".join("\\" + char if char in '\\;,:"' else char for char in str(value))


def wifi_qr(
    ssid: str,
    psk: str,
    security: str = "WPA",
    hidden: bool = False,
) -> str:
    """Return the ZXing WIFI: payload for qrencode."""
    hidden_value = "true" if str(hidden).lower() in ("1", "true", "yes", "on") else "false"
    return (
        f"WIFI:T:{security};S:{_escape(ssid)};P:{_escape(psk)};"
        f"H:{hidden_value};;"
    )


class FilterModule:
    def filters(self):
        return {"wifi_qr": wifi_qr}
