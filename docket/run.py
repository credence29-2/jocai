"""Run Docket from source: python run.py [file.pdf ...]"""

import multiprocessing
import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "src"))

if __name__ == "__main__":
    multiprocessing.freeze_support()
    from docket.app import main

    sys.exit(main())
