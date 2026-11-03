"""Shared code for the MGS2 restorations: file formats, the scenario compiler
driver, asset registration and local configuration.

Scripts in restorations/ put `<repo>/lib` on sys.path and import from here.
"""
import sys as _sys

# Scenario names are Japanese. A Windows console or pipe defaults to a legacy code
# page that can't print them, so say them in UTF-8 rather than crash mid-build.
for _s in (_sys.stdout, _sys.stderr):
    if _s and hasattr(_s, 'reconfigure') and (_s.encoding or '').lower().replace('-', '') != 'utf8':
        _s.reconfigure(encoding='utf-8', errors='backslashreplace')
