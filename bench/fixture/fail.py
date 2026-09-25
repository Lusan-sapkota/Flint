import os
import sys

if not os.environ.get("STOCKROOM_TOKEN"):
    print("error: STOCKROOM_TOKEN is not set", file=sys.stderr)
    sys.exit(2)
print("connected")
