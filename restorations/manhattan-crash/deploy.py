#!/usr/bin/env python3
"""Copy the built update/ folder into the game (MGS2_GAME), reversibly. See mgs2/deploy.py."""
import pathlib
import sys

HERE = pathlib.Path(__file__).resolve().parent
sys.path.insert(0, str(HERE.parents[1] / 'lib'))

from mgs2 import config, deploy  # noqa: E402

if __name__ == '__main__':
    deploy.deploy(config.build_dir() / 'manhattan-crash' / 'update', 'manhattan')
